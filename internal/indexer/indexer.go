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

	syncMu sync.Mutex
	syncs  map[int64]*syncEntry // channelID
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Indexer {
	return &Indexer{
		cfg:             cfg,
		db:              database,
		tgmgr:           mgr,
		discoverRunning: map[int64]struct{}{},
		syncs:           map[int64]*syncEntry{},
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

	// Only two dialog kinds are kept: broadcast channels (频道) and supergroups
	// (群组/megagroup, incl. forum groups whose topics we browse). Basic groups
	// (InputPeerChat) and private chats/bots (InputPeerUser) are intentionally
	// skipped — they are not browsable media sources here.
	return query.GetDialogs(raw).BatchSize(100).ForEach(ctx, func(ctx context.Context, e dialogs.Elem) error {
		p, ok := e.Peer.(*tg.InputPeerChannel)
		if !ok {
			return nil
		}
		ch, ok := e.Entities.Channel(p.ChannelID)
		if !ok {
			return nil
		}
		// Forums are flattened to megagroup at discovery; per-topic child rows
		// are populated by the (separate) topic-discovery path.
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
		return nil
	})
}
