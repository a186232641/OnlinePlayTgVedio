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

type Indexer struct {
	cfg   *config.Config
	db    *db.DB
	tgmgr *tgmanager.Manager

	mu      sync.Mutex
	running map[int64]context.CancelFunc
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Indexer {
	return &Indexer{
		cfg:     cfg,
		db:      database,
		tgmgr:   mgr,
		running: map[int64]context.CancelFunc{},
	}
}

// Trigger starts an index job for userID in the background. Idempotent — a
// second call while a job is running is ignored.
func (i *Indexer) Trigger(userID int64) {
	i.mu.Lock()
	if _, ok := i.running[userID]; ok {
		i.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	i.running[userID] = cancel
	i.mu.Unlock()

	go func() {
		defer func() {
			i.mu.Lock()
			delete(i.running, userID)
			i.mu.Unlock()
		}()
		err := i.run(ctx, userID)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
			slog.Warn("index job failed", "user_id", userID, "err", err)
		} else {
			slog.Info("index job done", "user_id", userID)
		}
		_ = i.db.FinishIndexJob(context.Background(), userID, errMsg)
	}()
}

// Cancel stops a running index job.
func (i *Indexer) Cancel(userID int64) {
	i.mu.Lock()
	cancel, ok := i.running[userID]
	delete(i.running, userID)
	i.mu.Unlock()
	if ok {
		cancel()
	}
}

func (i *Indexer) run(ctx context.Context, userID int64) error {
	cli, err := i.tgmgr.ClientFor(userID)
	if err != nil {
		return err
	}
	if err := i.db.StartIndexJob(ctx, userID); err != nil {
		return err
	}
	raw := cli.API

	// Step 1: collect channel-type dialogs.
	type chanDlg struct {
		channel *tg.Channel
		peer    tg.InputPeerClass
	}
	var collected []chanDlg
	if err := query.GetDialogs(raw).BatchSize(100).ForEach(ctx, func(ctx context.Context, e dialogs.Elem) error {
		switch p := e.Peer.(type) {
		case *tg.InputPeerChannel:
			ch, ok := e.Entities.Channel(p.ChannelID)
			if !ok {
				return nil
			}
			collected = append(collected, chanDlg{channel: ch, peer: p})
		}
		return nil
	}); err != nil {
		return fmt.Errorf("get dialogs: %w", err)
	}
	slog.Info("collected channels", "user_id", userID, "count", len(collected))

	if err := i.db.UpdateIndexProgress(ctx, userID, len(collected), 0, 0); err != nil {
		return err
	}

	totalVideos := 0
	dl := downloader.NewDownloader()

	for idx, cd := range collected {
		channelRowID, err := i.db.UpsertChannel(ctx, &db.Channel{
			UserID:      userID,
			TGChannelID: cd.channel.ID,
			AccessHash:  cd.channel.AccessHash,
			Title:       cd.channel.Title,
			Username:    cd.channel.Username,
		})
		if err != nil {
			return fmt.Errorf("upsert channel %d: %w", cd.channel.ID, err)
		}

		videosInCh := 0
		err = query.Messages(raw).GetHistory(cd.peer).BatchSize(100).ForEach(ctx, func(ctx context.Context, e messages.Elem) error {
			msg, ok := e.Msg.(*tg.Message)
			if !ok {
				return nil
			}
			doc := videoDocFromMessage(msg)
			if doc == nil {
				return nil
			}
			width, height, dur := videoMetadata(doc)

			sentAt := time.Unix(int64(msg.Date), 0).UTC()
			v := &db.Video{
				UserID:        userID,
				ChannelID:     channelRowID,
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

			// download thumbnail (best-effort, never fatal).
			if path, err := i.fetchThumb(ctx, dl, raw, doc); err == nil && path != "" {
				_ = i.db.UpdateVideoThumb(ctx, vid, path)
			}

			videosInCh++
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("history iteration failed", "channel", cd.channel.Title, "err", err)
		}
		totalVideos += videosInCh
		_ = i.db.MarkChannelIndexed(ctx, channelRowID, videosInCh)
		_ = i.db.UpdateIndexProgress(ctx, userID, len(collected), idx+1, totalVideos)

		if errors.Is(err, context.Canceled) {
			return ctx.Err()
		}
	}
	return nil
}

// videoDocFromMessage returns the video Document carried by a message, or nil.
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
