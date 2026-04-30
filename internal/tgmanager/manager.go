package tgmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gotd/contrib/bg"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgsession"
)

// Manager owns one persistent gotd client per active user.
type Manager struct {
	cfg *config.Config
	db  *db.DB

	mu      sync.RWMutex
	clients map[int64]*entry
}

type entry struct {
	client *telegram.Client
	api    *tg.Client
	stop   bg.StopFunc
}

// Client bundles the high-level + low-level handles a caller needs.
type Client struct {
	Telegram *telegram.Client
	API      *tg.Client
}

func New(cfg *config.Config, database *db.DB) *Manager {
	return &Manager{
		cfg:     cfg,
		db:      database,
		clients: map[int64]*entry{},
	}
}

// RestoreActive starts a client for every user whose tg_session is active.
// Failures are logged but do not abort startup.
func (m *Manager) RestoreActive(ctx context.Context) error {
	ids, err := m.db.ListActiveTGSessionUsers(ctx)
	if err != nil {
		return fmt.Errorf("list active sessions: %w", err)
	}
	for _, uid := range ids {
		if err := m.Start(ctx, uid); err != nil {
			slog.Warn("restore tg client failed", "user_id", uid, "err", err)
		}
	}
	slog.Info("tgmanager restored", "count", len(ids))
	return nil
}

// Start brings up a client for the given user (idempotent).
func (m *Manager) Start(ctx context.Context, userID int64) error {
	m.mu.Lock()
	if _, ok := m.clients[userID]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	storage := &tgsession.Storage{DB: m.db, UserID: userID, MasterKey: m.cfg.MasterKey}

	waiter := floodwait.NewWaiter().WithMaxRetries(5)

	client := telegram.NewClient(m.cfg.TgAPIID, m.cfg.TgAPIHash, telegram.Options{
		SessionStorage: storage,
		Middlewares: []telegram.Middleware{
			waiter,
		},
	})

	stop, err := bg.Connect(client)
	if err != nil {
		return fmt.Errorf("bg.Connect: %w", err)
	}

	// Quick auth probe so we don't keep a client whose session is broken.
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	if _, err := client.Self(probeCtx); err != nil {
		cancel()
		_ = stop()
		return fmt.Errorf("auth probe: %w", err)
	}
	cancel()

	m.mu.Lock()
	m.clients[userID] = &entry{client: client, api: client.API(), stop: stop}
	m.mu.Unlock()

	slog.Info("tg client started", "user_id", userID)
	return nil
}

// Stop tears down the client for a user (no-op if not running).
func (m *Manager) Stop(userID int64) error {
	m.mu.Lock()
	e, ok := m.clients[userID]
	if ok {
		delete(m.clients, userID)
	}
	m.mu.Unlock()
	if !ok || e.stop == nil {
		return nil
	}
	return e.stop()
}

// ClientFor returns the active client for a user, or an error if none.
func (m *Manager) ClientFor(userID int64) (*Client, error) {
	m.mu.RLock()
	e, ok := m.clients[userID]
	m.mu.RUnlock()
	if !ok {
		return nil, errors.New("no telegram client for user")
	}
	return &Client{Telegram: e.client, API: e.api}, nil
}

// Shutdown stops every client. Call before process exit.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for uid, e := range m.clients {
		if e.stop != nil {
			if err := e.stop(); err != nil {
				slog.Warn("stop client", "user_id", uid, "err", err)
			}
		}
	}
	m.clients = map[int64]*entry{}
}
