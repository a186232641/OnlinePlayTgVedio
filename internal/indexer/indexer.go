package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

// Indexer runs two kinds of background work per tg_session:
//
//	discover — enumerate dialogs (and forum topics) into the channels table.
//	          fast, runs once on bind and on demand.
//
//	worker  — pick one channel that the user has flipped index_enabled=true on,
//	          serially scan its message history into the videos table.
//	          one worker per session ⇒ at most one channel scan in flight per
//	          account, which keeps us well under FLOOD_WAIT thresholds.
type Indexer struct {
	cfg   *config.Config
	db    *db.DB
	tgmgr *tgmanager.Manager

	mu              sync.Mutex
	discoverRunning map[int64]struct{}      // sessionID
	workerNudge     map[int64]chan struct{} // sessionID → buffered(1) wakeup channel
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Indexer {
	return &Indexer{
		cfg:             cfg,
		db:              database,
		tgmgr:           mgr,
		discoverRunning: map[int64]struct{}{},
		workerNudge:     map[int64]chan struct{}{},
	}
}

// EnsureWorker idempotently spins up the per-session worker goroutine. Call
// it whenever a session becomes active (on startup restore, on new bind).
func (i *Indexer) EnsureWorker(sessionID int64) {
	i.mu.Lock()
	if _, ok := i.workerNudge[sessionID]; ok {
		i.mu.Unlock()
		return
	}
	nudge := make(chan struct{}, 1)
	i.workerNudge[sessionID] = nudge
	i.mu.Unlock()
	go i.workerLoop(sessionID, nudge)
}

// NudgeWorker wakes the session's worker goroutine to re-check its queue.
func (i *Indexer) NudgeWorker(sessionID int64) {
	i.mu.Lock()
	nudge, ok := i.workerNudge[sessionID]
	i.mu.Unlock()
	if !ok {
		return
	}
	select {
	case nudge <- struct{}{}:
	default:
	}
}

// TriggerDiscover starts a discover pass for the given session if one is not
// already running.
func (i *Indexer) TriggerDiscover(sessionID int64) {
	i.mu.Lock()
	if _, ok := i.discoverRunning[sessionID]; ok {
		i.mu.Unlock()
		return
	}
	i.discoverRunning[sessionID] = struct{}{}
	i.mu.Unlock()

	go func() {
		ctx := context.Background()
		err := i.discover(ctx, sessionID)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
			slog.Warn("discover failed", "session_id", sessionID, "err", err)
		} else {
			slog.Info("discover done", "session_id", sessionID)
		}
		_ = i.db.FinishDiscover(ctx, sessionID, errMsg)

		i.mu.Lock()
		delete(i.discoverRunning, sessionID)
		i.mu.Unlock()
	}()
}

// ----- discover -----

func (i *Indexer) discover(ctx context.Context, sessionID int64) error {
	cli, err := i.tgmgr.ClientForSession(sessionID)
	if err != nil {
		return err
	}
	if err := i.db.StartDiscover(ctx, sessionID); err != nil {
		return err
	}
	raw := cli.API
	userID := cli.UserID

	type discoveredForum struct {
		channelRowID int64
		ch           *tg.Channel
	}
	var forums []discoveredForum

	err = query.GetDialogs(raw).BatchSize(100).ForEach(ctx, func(ctx context.Context, e dialogs.Elem) error {
		switch p := e.Peer.(type) {
		case *tg.InputPeerChannel:
			ch, ok := e.Entities.Channel(p.ChannelID)
			if !ok {
				return nil
			}
			kind := db.DialogKindChannel
			switch {
			case ch.Forum:
				kind = db.DialogKindForum
			case ch.Megagroup:
				kind = db.DialogKindMegagroup
			}
			rowID, err := i.db.UpsertChannel(ctx, &db.Channel{
				UserID:      userID,
				TGSessionID: sessionID,
				TGChannelID: ch.ID,
				AccessHash:  ch.AccessHash,
				Title:       ch.Title,
				Username:    ch.Username,
				DialogKind:  kind,
			})
			if err != nil {
				return fmt.Errorf("upsert channel %d: %w", ch.ID, err)
			}
			if ch.Forum {
				forums = append(forums, discoveredForum{channelRowID: rowID, ch: ch})
			}
		case *tg.InputPeerChat:
			chat, ok := e.Entities.Chat(p.ChatID)
			if !ok {
				return nil
			}
			_, err := i.db.UpsertChannel(ctx, &db.Channel{
				UserID:      userID,
				TGSessionID: sessionID,
				TGChannelID: chat.ID,
				AccessHash:  0,
				Title:       chat.Title,
				DialogKind:  db.DialogKindGroup,
			})
			if err != nil {
				return fmt.Errorf("upsert chat %d: %w", chat.ID, err)
			}
		case *tg.InputPeerUser:
			usr, ok := e.Entities.User(p.UserID)
			if !ok {
				return nil
			}
			title := usr.Username
			if title == "" {
				title = fmt.Sprintf("%s %s", usr.FirstName, usr.LastName)
			}
			_, err := i.db.UpsertChannel(ctx, &db.Channel{
				UserID:      userID,
				TGSessionID: sessionID,
				TGChannelID: usr.ID,
				AccessHash:  usr.AccessHash,
				Title:       title,
				Username:    usr.Username,
				DialogKind:  db.DialogKindUser,
			})
			if err != nil {
				return fmt.Errorf("upsert user %d: %w", usr.ID, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("get dialogs: %w", err)
	}

	// For each forum group, enumerate topics into separate channel rows.
	for _, f := range forums {
		if err := i.discoverTopics(ctx, sessionID, userID, f.channelRowID, f.ch); err != nil {
			slog.Warn("discover topics", "channel", f.ch.Title, "err", err)
		}
	}
	return nil
}

func (i *Indexer) discoverTopics(ctx context.Context, sessionID, userID, parentRowID int64, ch *tg.Channel) error {
	cli, err := i.tgmgr.ClientForSession(sessionID)
	if err != nil {
		return err
	}
	peer := &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
	resp, err := cli.API.MessagesGetForumTopics(ctx, &tg.MessagesGetForumTopicsRequest{
		Peer:  peer,
		Limit: 100,
	})
	if err != nil {
		return err
	}
	for _, t := range resp.Topics {
		topic, ok := t.(*tg.ForumTopic)
		if !ok {
			continue
		}
		topicID := int32(topic.ID)
		_, err := i.db.UpsertChannel(ctx, &db.Channel{
			UserID:          userID,
			TGSessionID:     sessionID,
			TGChannelID:     ch.ID, // parent forum's tg id
			AccessHash:      ch.AccessHash,
			Title:           topic.Title,
			DialogKind:      db.DialogKindTopic,
			ParentChannelID: &parentRowID,
			TopicID:         &topicID,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ----- per-channel index worker -----

func (i *Indexer) workerLoop(sessionID int64, nudge <-chan struct{}) {
	ctx := context.Background()
	for {
		ch, err := i.db.ClaimNextChannelToIndex(ctx, sessionID)
		if errors.Is(err, db.ErrNotFound) {
			// queue empty — wait for either a nudge or a ticker tick
			select {
			case <-nudge:
				continue
			case <-time.After(2 * time.Minute):
				continue
			}
		}
		if err != nil {
			slog.Warn("claim next channel", "session_id", sessionID, "err", err)
			time.Sleep(15 * time.Second)
			continue
		}
		if err := i.indexChannel(ctx, sessionID, ch); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.Warn("index channel failed", "channel_id", ch.ID, "err", err)
			_ = i.db.SetChannelIndexStatus(ctx, ch.ID, db.IndexStatusFailed, err.Error())
		}
	}
}

func (i *Indexer) indexChannel(ctx context.Context, sessionID int64, ch *db.Channel) error {
	cli, err := i.tgmgr.ClientForSession(sessionID)
	if err != nil {
		return err
	}
	raw := cli.API
	userID := cli.UserID

	peer, err := inputPeerForChannel(ch)
	if err != nil {
		return err
	}

	dl := downloader.NewDownloader()

	collect := func(msg *tg.Message) error {
		doc := videoDocFromMessage(msg)
		if doc == nil {
			return nil
		}
		width, height, dur := videoMetadata(doc)
		sentAt := time.Unix(int64(msg.Date), 0).UTC()
		v := &db.Video{
			UserID:        userID,
			ChannelID:     ch.ID,
			TGMessageID:   int64(msg.ID),
			TGDocID:       doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
			Mime:          doc.MimeType,
			SizeBytes:     doc.Size,
			DurationSec:   dur,
			Width:         width,
			Height:        height,
			Caption:       msg.Message,
			SentAt:        &sentAt,
		}
		vid, err := i.db.UpsertVideo(ctx, v)
		if err != nil {
			return fmt.Errorf("upsert video: %w", err)
		}
		if path, err := i.fetchThumb(ctx, dl, raw, doc); err == nil && path != "" {
			_ = i.db.UpdateVideoThumb(ctx, vid, path)
		}
		return nil
	}

	count := 0

	if ch.DialogKind == db.DialogKindTopic && ch.TopicID != nil {
		err = query.Messages(raw).GetReplies(peer).MsgID(int(*ch.TopicID)).BatchSize(100).
			ForEach(ctx, func(ctx context.Context, e messages.Elem) error {
				msg, ok := e.Msg.(*tg.Message)
				if !ok {
					return nil
				}
				if err := collect(msg); err != nil {
					return err
				}
				count++
				return nil
			})
	} else {
		err = query.Messages(raw).GetHistory(peer).BatchSize(100).
			ForEach(ctx, func(ctx context.Context, e messages.Elem) error {
				msg, ok := e.Msg.(*tg.Message)
				if !ok {
					return nil
				}
				if err := collect(msg); err != nil {
					return err
				}
				count++
				return nil
			})
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	if err := i.db.MarkChannelIndexed(ctx, ch.ID, count); err != nil {
		return err
	}
	slog.Info("channel indexed", "channel_id", ch.ID, "title", ch.Title, "videos", count)
	return nil
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

func videoDocFromMessage(msg *tg.Message) *tg.Document {
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok {
		return nil
	}
	for _, attr := range doc.Attributes {
		if _, ok := attr.(*tg.DocumentAttributeVideo); ok {
			return doc
		}
	}
	return nil
}

func videoMetadata(doc *tg.Document) (width, height, durationSec int) {
	for _, attr := range doc.Attributes {
		if a, ok := attr.(*tg.DocumentAttributeVideo); ok {
			return a.W, a.H, int(a.Duration)
		}
	}
	return 0, 0, 0
}
