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
)

// SyncState is per-channel sync progress, kept in memory only.
type SyncState struct {
	Running bool   `json:"running"`
	Phase   string `json:"phase,omitempty"` // "syncing" while a run is active
	// Walked is the number of messages scanned (video or not); Imported/Skipped
	// move live as we write, because sync now streams to the DB batch by batch
	// instead of buffering the whole history first.
	Walked     int       `json:"walked"`
	Imported   int       `json:"imported"`
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

	go i.runSync(ch, cli.API, st)
	return st.snapshot(), nil
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

	peer, err := inputPeerForChannel(ch)
	if err != nil {
		st.update(func(s *SyncState) { s.LastError = err.Error() })
		return
	}

	maxSeen64, _ := i.db.MaxTGMsgID(ctx, ch.ID, ch.UserID)
	maxSeen := int(maxSeen64)
	slog.Info("sync start",
		"channel_id", ch.ID, "title", ch.Title,
		"max_seen", maxSeen, "history_complete", ch.HistoryComplete,
	)

	// Probe: a single getHistory call. 0 messages almost always means the
	// access_hash is stale or we lost membership — bail with a clear message
	// instead of silently walking nothing.
	probe, perr := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: 1})
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
		if _, err := i.walkHistory(ctx, api, peer, ch, st, 0, maxSeen); err != nil {
			i.reportSyncErr(ch, st, "incremental", err)
			return
		}
	}

	// Phase B — backfill: walk older history below our oldest message, in
	// batches, until we hit the very bottom. Resumable: progress is written as
	// we go, and MIN(tg_msg_id) is the cursor next time. Skipped once complete.
	if !ch.HistoryComplete {
		minSeen64, _ := i.db.MinTGMsgID(ctx, ch.ID, ch.UserID)
		bottom, err := i.walkHistory(ctx, api, peer, ch, st, int(minSeen64), 0)
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
	ctx context.Context, api *tg.Client, peer tg.InputPeerClass,
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

		resp, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
			Peer:     peer,
			OffsetID: offsetID,
			MinID:    minID,
			Limit:    pageSize,
		})
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

// writeMsg upserts one message's video (if it has one), updating live counters.
func (i *Indexer) writeMsg(ctx context.Context, ch *db.Channel, m *tg.Message, st *syncEntry) error {
	v := videoFromTGMessage(ch, m)
	if v == nil {
		st.update(func(s *SyncState) { s.Skipped++ })
		return nil
	}
	if _, err := i.db.UpsertVideo(ctx, v); err != nil {
		return fmt.Errorf("upsert msg %d: %w", m.ID, err)
	}
	st.update(func(s *SyncState) { s.Imported++ })
	return nil
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

		// Sync gives us the locator straight from TG → first play won't refresh.
		TGDocID:       doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
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
