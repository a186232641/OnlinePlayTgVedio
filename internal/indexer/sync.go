package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmedia"
)

// SyncState is per-channel sync progress, kept in memory only.
type SyncState struct {
	Running bool   `json:"running"`
	Phase   string `json:"phase,omitempty"` // "syncing" while a run is active
	// Walked is the number of messages scanned (video or not); Imported/Skipped
	// move live as we write, because sync now streams to the DB batch by batch
	// instead of buffering the whole history first.
	Walked   int `json:"walked"`
	Imported int `json:"imported"`
	// Videos/Photos break Imported down by media kind (they always sum to it).
	Videos     int       `json:"videos"`
	Photos     int       `json:"photos"`
	Skipped    int       `json:"skipped"`
	LastError  string    `json:"last_error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type syncEntry struct {
	mu    sync.Mutex
	state SyncState
}

func (e *syncEntry) snapshot() SyncState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func (e *syncEntry) update(fn func(*SyncState)) {
	e.mu.Lock()
	fn(&e.state)
	e.mu.Unlock()
}

// SyncStart kicks off (idempotently) a goroutine that pulls the channel's
// history and upserts each video row. It is incremental (only messages newer
// than what's stored) AND resumable (it backfills older history in batches,
// so a crash mid-sync resumes from the stored MIN/MAX cursor next time).
func (i *Indexer) SyncStart(parentCtx context.Context, channelID, userID int64) (SyncState, error) {
	i.syncMu.Lock()
	if st, ok := i.syncs[channelID]; ok && st.snapshot().Running {
		i.syncMu.Unlock()
		return st.snapshot(), nil
	}
	ch, err := i.db.ChannelByID(parentCtx, channelID, userID)
	if err != nil {
		i.syncMu.Unlock()
		return SyncState{}, err
	}
	cli, err := i.tgmgr.ClientForSession(ch.TGSessionID)
	if err != nil {
		i.syncMu.Unlock()
		return SyncState{}, fmt.Errorf("tg client: %w", err)
	}
	st := &syncEntry{state: SyncState{Running: true, StartedAt: time.Now()}}
	i.syncs[channelID] = st
	i.syncMu.Unlock()

	// A forum group has no browsable history of its own — every message belongs
	// to a topic. Syncing it means "refresh the topic list, then sync each
	// topic", which is what runForumSync does.
	if ch.IsForum && ch.DialogKind != db.DialogKindTopic {
		go i.runForumSync(ch, cli.API, st)
		return st.snapshot(), nil
	}
	go i.runSync(ch, cli.API, st)
	return st.snapshot(), nil
}

// runForumSync re-enumerates a forum group's topics and then syncs them one by
// one, aggregating their counters onto the group's own sync state so the UI can
// watch a single progress line. Topics are synced sequentially on purpose: they
// all share one TG session, and parallel history walks are the fastest way to
// earn a FLOOD_WAIT.
func (i *Indexer) runForumSync(ch *db.Channel, api *tg.Client, st *syncEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	defer cancel()
	defer func() {
		st.update(func(s *SyncState) {
			s.Running = false
			s.Phase = ""
			s.FinishedAt = time.Now()
		})
		_ = i.db.MarkChannelIndexed(ctx, ch.ID)
	}()

	st.update(func(s *SyncState) { s.Phase = "topics" })
	if _, err := i.discoverTopics(ctx, api, ch); err != nil {
		slog.Warn("forum sync: topic discovery failed", "channel_id", ch.ID, "err", err)
		st.update(func(s *SyncState) { s.LastError = "枚举话题失败: " + err.Error() })
		return
	}
	topics, err := i.db.ListTopics(ctx, ch.ID, ch.UserID)
	if err != nil {
		st.update(func(s *SyncState) { s.LastError = err.Error() })
		return
	}
	if len(topics) == 0 {
		st.update(func(s *SyncState) { s.LastError = "该群组没有话题(或话题对当前账号不可见)" })
		return
	}

	for idx := range topics {
		if ctx.Err() != nil {
			break
		}
		t := topics[idx]
		st.update(func(s *SyncState) {
			s.Phase = fmt.Sprintf("话题 %d/%d: %s", idx+1, len(topics), t.Title)
		})

		// Reuse the per-channel sync entry so the topic's own detail page shows
		// live progress too; skip a topic that is already being synced manually.
		i.syncMu.Lock()
		if cur, ok := i.syncs[t.ID]; ok && cur.snapshot().Running {
			i.syncMu.Unlock()
			continue
		}
		sub := &syncEntry{state: SyncState{Running: true, StartedAt: time.Now()}}
		i.syncs[t.ID] = sub
		i.syncMu.Unlock()

		i.runSync(&t, api, sub) // synchronous: one topic at a time

		snap := sub.snapshot()
		st.update(func(s *SyncState) {
			s.Walked += snap.Walked
			s.Imported += snap.Imported
			s.Videos += snap.Videos
			s.Photos += snap.Photos
			s.Skipped += snap.Skipped
		})
	}
	slog.Info("forum sync done", "channel_id", ch.ID, "topics", len(topics))
}

func (i *Indexer) SyncStatus(channelID int64) SyncState {
	i.syncMu.Lock()
	defer i.syncMu.Unlock()
	if st, ok := i.syncs[channelID]; ok {
		return st.snapshot()
	}
	return SyncState{}
}

func (i *Indexer) runSync(ch *db.Channel, api *tg.Client, st *syncEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	defer func() {
		st.update(func(s *SyncState) {
			s.Running = false
			s.Phase = ""
			s.FinishedAt = time.Now()
		})
		// Recount video_count from the actual rows (never the per-run delta).
		_ = i.db.MarkChannelIndexed(ctx, ch.ID)
	}()

	fetch, err := i.fetcherFor(api, ch)
	if err != nil {
		st.update(func(s *SyncState) { s.LastError = err.Error() })
		return
	}

	maxSeen64, _ := i.db.MaxMsgIDForChannel(ctx, ch.ID, ch.UserID)
	maxSeen := int(maxSeen64)
	slog.Info("sync start",
		"channel_id", ch.ID, "title", ch.Title,
		"max_seen", maxSeen, "history_complete", ch.HistoryComplete,
	)

	// Probe: a single getHistory call. 0 messages almost always means the
	// access_hash is stale or we lost membership — bail with a clear message
	// instead of silently walking nothing.
	probe, perr := fetch(ctx, 0, 0, 1)
	if perr != nil {
		slog.Warn("sync probe failed", "channel_id", ch.ID, "err", perr)
		st.update(func(s *SyncState) { s.LastError = "probe: " + perr.Error() })
		return
	}
	if len(extractMessages(probe)) == 0 {
		st.update(func(s *SyncState) {
			s.LastError = "TG 返回 0 条历史消息(可能 access_hash 已过期或失去访问权限,试着在 TG 账号管理页'重新发现')"
		})
		return
	}

	st.update(func(s *SyncState) { s.Phase = "syncing" })

	// Phase A — incremental: pull messages newer than maxSeen (top of history).
	// Skipped when the channel is empty; the backfill below covers that case.
	if maxSeen > 0 {
		if _, err := i.walkHistory(ctx, fetch, ch, st, 0, maxSeen); err != nil {
			i.reportSyncErr(ch, st, "incremental", err)
			return
		}
	}

	// Phase B — backfill: walk older history below our oldest message, in
	// batches, until we hit the very bottom. Resumable: progress is written as
	// we go, and MIN(tg_msg_id) is the cursor next time. Skipped once complete.
	if !ch.HistoryComplete {
		minSeen64, _ := i.db.MinMsgIDForChannel(ctx, ch.ID, ch.UserID)
		bottom, err := i.walkHistory(ctx, fetch, ch, st, int(minSeen64), 0)
		if err != nil {
			i.reportSyncErr(ch, st, "backfill", err)
			return
		}
		if bottom {
			if err := i.db.SetHistoryComplete(ctx, ch.ID); err == nil {
				ch.HistoryComplete = true
			}
		}
	}

	snap := st.snapshot()
	if snap.Imported == 0 && snap.Skipped == 0 && ch.HistoryComplete {
		st.update(func(s *SyncState) { s.LastError = "已是最新(没有新消息)" })
	}
	slog.Info("sync done",
		"channel_id", ch.ID,
		"imported", snap.Imported, "skipped", snap.Skipped, "walked", snap.Walked,
		"history_complete", ch.HistoryComplete,
	)
}

// reportSyncErr records a phase error. A timeout/cancel is benign — progress is
// already persisted and the next run resumes from the stored cursor — so it
// gets an informational message rather than a scary "failed".
func (i *Indexer) reportSyncErr(ch *db.Channel, st *syncEntry, phase string, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		slog.Info("sync interrupted (will resume next run)", "channel_id", ch.ID, "phase", phase)
		st.update(func(s *SyncState) {
			s.LastError = "已是最新? 本轮同步达时间上限,已保存进度,下次会从断点继续"
		})
		return
	}
	slog.Warn("sync phase failed", "channel_id", ch.ID, "phase", phase, "err", err)
	st.update(func(s *SyncState) { s.LastError = err.Error() })
}

// walkHistory pages messages.getHistory newest→oldest, starting just below
// startOffsetID (0 = from the newest message) and bounded below by minID (0 =
// none). It writes each video to the DB as it goes, so a crash leaves partial
// progress that the next run resumes from. Returns reachedBottom=true when
// history is exhausted (an empty page), false when stopped at a bound / error.
func (i *Indexer) walkHistory(
	ctx context.Context, fetch pageFetcher,
	ch *db.Channel, st *syncEntry, startOffsetID, minID int,
) (bool, error) {
	const pageSize = 100
	const logEvery = 1000
	offsetID := startOffsetID
	sinceLog := 0
	lastTick := time.Now()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		resp, err := fetch(ctx, offsetID, minID, pageSize)
		if err != nil {
			return false, err
		}
		msgs := extractMessages(resp)
		if len(msgs) == 0 {
			return true, nil // exhausted
		}

		batchMin := 0
		for _, mc := range msgs {
			if id := mc.GetID(); id > 0 && (batchMin == 0 || id < batchMin) {
				batchMin = id
			}
			m, ok := mc.(*tg.Message)
			if !ok {
				continue
			}
			if err := i.writeMsg(ctx, ch, m, st); err != nil {
				return false, err
			}
		}

		st.update(func(s *SyncState) { s.Walked += len(msgs) })
		if sinceLog += len(msgs); sinceLog >= logEvery {
			snap := st.snapshot()
			slog.Info("sync progress",
				"channel_id", ch.ID, "walked", snap.Walked,
				"imported", snap.Imported, "skipped", snap.Skipped,
				"cursor_msg_id", batchMin, "page_dur_ms", time.Since(lastTick).Milliseconds(),
			)
			sinceLog = 0
			lastTick = time.Now()
		}

		// Safety: if the cursor can't advance, stop rather than loop forever.
		if batchMin == 0 || batchMin == offsetID {
			return true, nil
		}
		offsetID = batchMin
	}
}

// writeMsg upserts one message's media (video or photo, whichever it carries),
// updating live counters. Messages with neither are counted as skipped.
func (i *Indexer) writeMsg(ctx context.Context, ch *db.Channel, m *tg.Message, st *syncEntry) error {
	if v := videoFromTGMessage(ch, m); v != nil {
		if _, err := i.db.UpsertVideo(ctx, v); err != nil {
			return fmt.Errorf("upsert video msg %d: %w", m.ID, err)
		}
		// Album member: spread the group's caption onto its silent siblings so all
		// of them are searchable, not just the one message that carried the text.
		if v.GroupedID != 0 {
			if err := i.db.PropagateGroupCaption(ctx, ch.UserID, ch.ID, v.GroupedID); err != nil {
				return fmt.Errorf("propagate caption grp %d: %w", v.GroupedID, err)
			}
		}
		st.update(func(s *SyncState) {
			s.Imported++
			s.Videos++
		})
		return nil
	}
	if p := photoFromTGMessage(ch, m); p != nil {
		if _, err := i.db.UpsertPhoto(ctx, p); err != nil {
			return fmt.Errorf("upsert photo msg %d: %w", m.ID, err)
		}
		if p.GroupedID != 0 {
			if err := i.db.PropagatePhotoGroupCaption(ctx, ch.UserID, ch.ID, p.GroupedID); err != nil {
				return fmt.Errorf("propagate photo caption grp %d: %w", p.GroupedID, err)
			}
		}
		st.update(func(s *SyncState) {
			s.Imported++
			s.Photos++
		})
		return nil
	}
	st.update(func(s *SyncState) { s.Skipped++ })
	return nil
}

// photoFromTGMessage maps an image message to a db.Photo. Returns nil when the
// message carries no photo.
//
// Both MessageMediaPhoto and a document with an image mime can show up; only
// the former is a real Telegram photo (with the size ladder we serve from).
// Image *documents* (someone sending a .png "as file") stay out of both tables
// for now — they are rare and would need the document locator path.
func photoFromTGMessage(ch *db.Channel, msg *tg.Message) *db.Photo {
	media, ok := msg.Media.(*tg.MessageMediaPhoto)
	if !ok {
		return nil
	}
	pc, ok := media.GetPhoto()
	if !ok {
		return nil
	}
	photo, ok := pc.AsNotEmpty()
	if !ok {
		return nil
	}
	full, thumb := tgmedia.PickPhotoSizes(photo.Sizes)
	if full.Type == "" {
		return nil // nothing downloadable (stripped-only placeholder)
	}

	sentAt := time.Unix(int64(msg.Date), 0).UTC()
	var edited *time.Time
	if msg.EditDate != 0 {
		t := time.Unix(int64(msg.EditDate), 0).UTC()
		edited = &t
	}

	return &db.Photo{
		UserID:    ch.UserID,
		ChannelID: ch.ID,

		TGMsgID:   int64(msg.ID),
		MsgType:   "message",
		Date:      &sentAt,
		Edited:    edited,
		FromName:  ch.Title,
		FromID:    fmt.Sprintf("channel%d", ch.TGChannelID),
		FileSize:  full.Bytes,
		Width:     full.W,
		Height:    full.H,
		Text:      msg.Message,
		GroupedID: msg.GroupedID,

		TGPhotoID:     photo.ID,
		AccessHash:    photo.AccessHash,
		FileReference: photo.FileReference,
		DCID:          photo.DCID,
		SizeType:      full.Type,
		ThumbSize:     thumb.Type,
	}
}

// videoFromTGMessage maps a TG message to a db.Video, mirroring the JSON
// import schema. Returns nil if the message has no video document.
func videoFromTGMessage(ch *db.Channel, msg *tg.Message) *db.Video {
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok {
		return nil
	}

	var (
		mediaType  string
		fileName   string
		w, h, dur  int
		hasVideo   bool
		isRound    bool
		isAnimated bool
	)
	for _, attr := range doc.Attributes {
		switch a := attr.(type) {
		case *tg.DocumentAttributeVideo:
			hasVideo = true
			w, h, dur = a.W, a.H, int(a.Duration)
			isRound = a.RoundMessage
		case *tg.DocumentAttributeAnimated:
			isAnimated = true
		case *tg.DocumentAttributeFilename:
			fileName = a.FileName
		}
	}
	switch {
	case hasVideo && isRound:
		mediaType = "video_message"
	case hasVideo:
		mediaType = "video_file"
	case isAnimated:
		mediaType = "animation"
	case strings.HasPrefix(strings.ToLower(doc.MimeType), "video/"):
		mediaType = "video_file"
	case hasVideoExt(fileName):
		// Plenty of large channels post videos as plain documents with no
		// video attribute and a generic mime (e.g. application/octet-stream).
		// Mirror the JSON importer's extension fallback so sync doesn't miss them.
		mediaType = "video_file"
	default:
		return nil
	}

	sentAt := time.Unix(int64(msg.Date), 0).UTC()
	var edited *time.Time
	if msg.EditDate != 0 {
		t := time.Unix(int64(msg.EditDate), 0).UTC()
		edited = &t
	}

	return &db.Video{
		UserID:    ch.UserID,
		ChannelID: ch.ID,

		TGMsgID:         int64(msg.ID),
		MsgType:         "message",
		Date:            &sentAt,
		Edited:          edited,
		FromName:        ch.Title,
		FromID:          fmt.Sprintf("channel%d", ch.TGChannelID),
		FileName:        fileName,
		FileSize:        doc.Size,
		MediaType:       mediaType,
		MimeType:        doc.MimeType,
		DurationSeconds: dur,
		Width:           w,
		Height:          h,
		Text:            msg.Message,
		GroupedID:       msg.GroupedID, // 0 when not part of an album

		// Sync gives us the locator straight from TG → first play won't refresh.
		TGDocID:       doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
		DCID:          doc.DCID,
		ThumbSize:     tgmedia.PickDocThumb(doc.Thumbs),
	}
}

// videoFileExts mirrors the JSON importer's list (handlers.videoExts): used as
// a last-resort signal when a document carries no video attribute and a
// non-video mime type.
var videoFileExts = []string{".mp4", ".mov", ".m4v", ".mkv", ".webm", ".avi", ".flv", ".ts", ".mpeg", ".mpg", ".3gp"}

func hasVideoExt(name string) bool {
	low := strings.ToLower(name)
	for _, ext := range videoFileExts {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return false
}

// pageFetcher pulls one page of messages newest→oldest: strictly below
// offsetID (0 = start at the newest message) and strictly above minID (0 =
// unbounded). Abstracting it is what lets one walk serve both a whole channel
// and a single forum topic.
type pageFetcher func(ctx context.Context, offsetID, minID, limit int) (tg.MessagesMessagesClass, error)

// fetcherFor picks the right history API for a channel row.
//
// Plain channels/groups page through messages.getHistory. A forum topic is not
// a peer of its own — its messages live in the parent group's history — so it
// pages through messages.search with top_msg_id set to the topic id. The filter
// is deliberately InputMessagesFilterEmpty rather than PhotoVideo: a lot of
// channels post videos as plain documents with a generic mime type, which the
// PhotoVideo filter drops server-side and which videoFromTGMessage's extension
// fallback is there to catch.
func (i *Indexer) fetcherFor(api *tg.Client, ch *db.Channel) (pageFetcher, error) {
	peer, err := inputPeerForChannel(ch)
	if err != nil {
		return nil, err
	}
	if ch.DialogKind == db.DialogKindTopic {
		if ch.TopicID == nil {
			return nil, fmt.Errorf("话题 %d 缺少 topic_id,请在群组页重新拉取话题列表", ch.ID)
		}
		topicID := int(*ch.TopicID)
		return func(ctx context.Context, offsetID, minID, limit int) (tg.MessagesMessagesClass, error) {
			return api.MessagesSearch(ctx, &tg.MessagesSearchRequest{
				Peer:     peer,
				Q:        "",
				Filter:   &tg.InputMessagesFilterEmpty{},
				TopMsgID: topicID,
				OffsetID: offsetID,
				MinID:    minID,
				Limit:    limit,
			})
		}, nil
	}
	return func(ctx context.Context, offsetID, minID, limit int) (tg.MessagesMessagesClass, error) {
		return api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
			Peer:     peer,
			OffsetID: offsetID,
			MinID:    minID,
			Limit:    limit,
		})
	}, nil
}

func inputPeerForChannel(c *db.Channel) (tg.InputPeerClass, error) {
	switch c.DialogKind {
	case db.DialogKindGroup:
		return &tg.InputPeerChat{ChatID: c.TGChannelID}, nil
	case db.DialogKindUser:
		return &tg.InputPeerUser{UserID: c.TGChannelID, AccessHash: c.AccessHash}, nil
	default:
		return &tg.InputPeerChannel{ChannelID: c.TGChannelID, AccessHash: c.AccessHash}, nil
	}
}

// extractMessages flattens the various MessagesMessagesClass response shapes
// into a single []MessageClass.
func extractMessages(resp tg.MessagesMessagesClass) []tg.MessageClass {
	switch r := resp.(type) {
	case *tg.MessagesMessages:
		return r.Messages
	case *tg.MessagesMessagesSlice:
		return r.Messages
	case *tg.MessagesChannelMessages:
		return r.Messages
	default:
		return nil
	}
}
