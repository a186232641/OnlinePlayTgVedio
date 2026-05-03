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

// SyncState describes a per-channel sync's progress, kept in memory only.
// Polled by the frontend via GET /api/channels/:id/sync.
type SyncState struct {
	Running    bool      `json:"running"`
	Imported   int       `json:"imported"`    // videos written so far
	Skipped    int       `json:"skipped"`     // non-video messages
	LastError  string    `json:"last_error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

// SyncStart kicks off (idempotently) a goroutine that pulls messages.getHistory
// for the channel and upserts each video row. If a sync is already running for
// the channel, returns its current state without spawning another.
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

// SyncStatus returns the current state, or an empty struct if never started.
func (i *Indexer) SyncStatus(channelID int64) SyncState {
	i.syncMu.Lock()
	defer i.syncMu.Unlock()
	if st, ok := i.syncs[channelID]; ok {
		return st.snapshot()
	}
	return SyncState{}
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

func (i *Indexer) runSync(ch *db.Channel, api *tg.Client, st *syncEntry) {
	// Use a fresh context detached from the HTTP request — sync may run for
	// minutes after the trigger response is gone.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	defer func() {
		st.update(func(s *SyncState) {
			s.Running = false
			s.FinishedAt = time.Now()
		})
		// Reflect totals on the channels row so the browse UI can show count.
		_ = i.db.MarkChannelIndexed(ctx, ch.ID, st.snapshot().Imported)
	}()

	peer, err := inputPeerForChannel(ch)
	if err != nil {
		st.update(func(s *SyncState) { s.LastError = err.Error() })
		return
	}

	// Incremental: only fetch messages newer than what's already in DB.
	// MaxTGMsgID returns 0 when the channel has no rows yet, which means
	// "fetch everything from the latest backwards".
	maxSeen, _ := i.db.MaxTGMsgID(ctx, ch.ID, ch.UserID)

	slog.Info("sync start",
		"channel_id", ch.ID, "title", ch.Title,
		"max_seen_msg_id", maxSeen, "tg_session_id", ch.TGSessionID,
	)

	q := query.Messages(api).GetHistory(peer).BatchSize(100)
	if maxSeen > 0 {
		q = q.OffsetID(0)
		// Iterate until we hit a message <= maxSeen, then stop early.
	}

	stopErr := errors.New("sync done")
	err = q.ForEach(ctx, func(_ context.Context, e messages.Elem) error {
		msg, ok := e.Msg.(*tg.Message)
		if !ok {
			return nil
		}
		if maxSeen > 0 && int64(msg.ID) <= maxSeen {
			return stopErr // hit known territory; nothing newer to fetch
		}
		v := videoFromTGMessage(ch, msg)
		if v == nil {
			st.update(func(s *SyncState) { s.Skipped++ })
			return nil
		}
		if _, err := i.db.UpsertVideo(ctx, v); err != nil {
			return fmt.Errorf("upsert msg=%d: %w", msg.ID, err)
		}
		st.update(func(s *SyncState) { s.Imported++ })
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
		"skipped", st.snapshot().Skipped,
	)
}

// videoFromTGMessage maps a TG message to our db.Video, mirroring the JSON
// import pathway. Returns nil if the message has no video document.
func videoFromTGMessage(ch *db.Channel, msg *tg.Message) *db.Video {
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok {
		return nil
	}

	mediaType := ""
	width, height, dur := 0, 0, 0
	fileName := ""
	hasVideoAttr := false
	isRound := false
	isAnimated := false
	for _, attr := range doc.Attributes {
		switch a := attr.(type) {
		case *tg.DocumentAttributeVideo:
			hasVideoAttr = true
			width, height, dur = a.W, a.H, int(a.Duration)
			isRound = a.RoundMessage
		case *tg.DocumentAttributeAnimated:
			isAnimated = true
		case *tg.DocumentAttributeFilename:
			fileName = a.FileName
		}
	}

	switch {
	case hasVideoAttr && isRound:
		mediaType = "video_message"
	case hasVideoAttr:
		mediaType = "video_file"
	case isAnimated:
		mediaType = "animation"
	case strings.HasPrefix(strings.ToLower(doc.MimeType), "video/"):
		mediaType = "video_file"
	default:
		return nil // not a video
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

		TGMsgID:           int64(msg.ID),
		MsgType:           "message",
		Date:              &sentAt,
		Edited:            edited,
		FromName:          ch.Title,
		FromID:            fmt.Sprintf("channel%d", ch.TGChannelID),
		File:              "", // not pulled
		FileName:          fileName,
		FileSize:          doc.Size,
		Thumbnail:         "",
		ThumbnailFileSize: 0,
		MediaType:         mediaType,
		MimeType:          doc.MimeType,
		DurationSeconds:   dur,
		Width:             width,
		Height:            height,
		Text:              msg.Message,
		TextEntities:      nil,

		// We have the locator straight from TG — first play won't need refresh.
		TGDocID:       doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
	}
}

// inputPeerForChannel picks the right input peer kind based on dialog_kind.
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
