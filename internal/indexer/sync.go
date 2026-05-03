package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
)

// SyncState is per-channel sync progress, kept in memory only.
type SyncState struct {
	Running    bool      `json:"running"`
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

// SyncStart kicks off (idempotently) a goroutine that pulls
// messages.getHistory for the channel and upserts each video row. Incremental:
// only fetches messages with id > MAX(tg_msg_id) already stored.
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
			s.FinishedAt = time.Now()
		})
		_ = i.db.MarkChannelIndexed(ctx, ch.ID, st.snapshot().Imported)
	}()

	peer, err := inputPeerForChannel(ch)
	if err != nil {
		st.update(func(s *SyncState) { s.LastError = err.Error() })
		return
	}

	maxSeen, _ := i.db.MaxTGMsgID(ctx, ch.ID, ch.UserID)
	slog.Info("sync start",
		"channel_id", ch.ID, "title", ch.Title,
		"tg_channel_id", ch.TGChannelID, "access_hash", ch.AccessHash,
		"dialog_kind", ch.DialogKind, "max_seen", maxSeen,
	)

	// Probe: directly call MessagesGetHistory once and log the raw count.
	// If this returns 0 messages, no point iterating — almost always means
	// the access_hash is stale or we lost membership in the channel.
	probe, perr := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
		Peer:  peer,
		Limit: 5,
	})
	if perr != nil {
		slog.Warn("sync probe failed", "channel_id", ch.ID, "err", perr)
		st.update(func(s *SyncState) { s.LastError = "probe: " + perr.Error() })
		return
	}
	probeMsgs := extractMessages(probe)
	probeLatest := int64(0)
	for _, mc := range probeMsgs {
		if m, ok := mc.(*tg.Message); ok && int64(m.ID) > probeLatest {
			probeLatest = int64(m.ID)
		}
	}
	slog.Info("sync probe ok",
		"channel_id", ch.ID,
		"resp_type", fmt.Sprintf("%T", probe),
		"messages", len(probeMsgs),
		"latest_msg_id", probeLatest,
	)
	if len(probeMsgs) == 0 {
		st.update(func(s *SyncState) {
			s.LastError = "TG 返回 0 条历史消息(可能 access_hash 已过期或失去访问权限,试着在 TG 账号管理页'重新发现')"
		})
		return
	}
	// Already up-to-date: TG's latest message is older than what we already
	// have. Report this distinctly so the UI doesn't show a confusing "0/0".
	if maxSeen > 0 && probeLatest > 0 && probeLatest <= maxSeen {
		slog.Info("sync up-to-date",
			"channel_id", ch.ID, "tg_latest", probeLatest, "max_seen", maxSeen)
		st.update(func(s *SyncState) {
			s.LastError = fmt.Sprintf("已是最新(本地已有到 msg_id=%d,TG 频道最新消息 id=%d)", maxSeen, probeLatest)
		})
		return
	}

	const progressEvery = 500 // log a progress line every N messages walked
	walked := 0
	lastTickAt := time.Now()

	stopErr := errors.New("done")
	err = query.Messages(api).GetHistory(peer).BatchSize(100).
		ForEach(ctx, func(_ context.Context, e messages.Elem) error {
			msg, ok := e.Msg.(*tg.Message)
			if !ok {
				return nil
			}
			if maxSeen > 0 && int64(msg.ID) <= maxSeen {
				return stopErr
			}
			v := videoFromTGMessage(ch, msg)
			if v == nil {
				st.update(func(s *SyncState) { s.Skipped++ })
			} else {
				if _, err := i.db.UpsertVideo(ctx, v); err != nil {
					return fmt.Errorf("upsert msg=%d: %w", msg.ID, err)
				}
				st.update(func(s *SyncState) { s.Imported++ })
			}
			walked++
			if walked%progressEvery == 0 {
				snap := st.snapshot()
				slog.Info("sync progress",
					"channel_id", ch.ID,
					"walked", walked,
					"imported", snap.Imported,
					"skipped", snap.Skipped,
					"current_msg_id", msg.ID,
					"page_dur_ms", time.Since(lastTickAt).Milliseconds(),
				)
				lastTickAt = time.Now()
			}
			return nil
		})

	if err != nil && !errors.Is(err, stopErr) && !errors.Is(err, context.Canceled) {
		slog.Warn("sync failed", "channel_id", ch.ID, "err", err)
		st.update(func(s *SyncState) { s.LastError = err.Error() })
		return
	}
	slog.Info("sync done",
		"channel_id", ch.ID,
		"imported", st.snapshot().Imported,
		"skipped", st.snapshot().Skipped)
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
		mediaType    string
		fileName     string
		w, h, dur    int
		hasVideo     bool
		isRound      bool
		isAnimated   bool
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
