package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

// Indexer is now a thin "discover dialogs" helper. The historical indexer
// worker (per-channel video crawl) was removed: JSON import is the only
// way videos get into the videos table now.
type Indexer struct {
	cfg   *config.Config
	db    *db.DB
	tgmgr *tgmanager.Manager

	mu              sync.Mutex
	discoverRunning map[int64]struct{} // sessionID
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Indexer {
	return &Indexer{
		cfg:             cfg,
		db:              database,
		tgmgr:           mgr,
		discoverRunning: map[int64]struct{}{},
	}
}

// TriggerDiscover kicks off (idempotently) a dialog enumeration for the given
// session. We need this to populate `channels` rows so JSON-imported videos
// can resolve channel access_hash for streaming.
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

	return query.GetDialogs(raw).BatchSize(100).ForEach(ctx, func(ctx context.Context, e dialogs.Elem) error {
		switch p := e.Peer.(type) {
		case *tg.InputPeerChannel:
			ch, ok := e.Entities.Channel(p.ChannelID)
			if !ok {
				return nil
			}
			// Forums are flattened to megagroup; topic discovery removed
			// because UI no longer supports per-topic operations.
			kind := db.DialogKindChannel
			if ch.Megagroup || ch.Forum {
				kind = db.DialogKindMegagroup
			}
			_, err := i.db.UpsertChannel(ctx, &db.Channel{
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
}
