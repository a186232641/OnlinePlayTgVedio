package tgmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gotd/contrib/bg"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmw"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgsession"
)

// recoveryMaxElapsed caps how long the recovery middleware keeps reconnecting
// for a single RPC before giving up — long enough to ride out a DC migration /
// brief network blip, short enough that a truly dead connection fails fast.
const recoveryMaxElapsed = 90 * time.Second

// recoveryBackoff builds a fresh exponential backoff for one RPC's recovery loop.
func recoveryBackoff() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.Multiplier = 1.1
	b.MaxInterval = 10 * time.Second
	b.MaxElapsedTime = recoveryMaxElapsed
	return b
}

// DCList returns the production DC list with any TG_DC_OVERRIDES applied. Each
// override is prepended for its DC id so gotd dials the given (current) IP
// before the built-in one, which goes stale when Telegram rotates addresses.
// Same DC id ⇒ same auth key, so overriding only the address needs no re-login.
func DCList(cfg *config.Config) dcs.List {
	list := dcs.Prod()
	if len(cfg.DCOverrides) == 0 {
		return list
	}
	opts := make([]tg.DCOption, 0, len(cfg.DCOverrides)+len(list.Options))
	for _, o := range cfg.DCOverrides {
		opts = append(opts, tg.DCOption{ID: o.ID, IPAddress: o.IP, Port: o.Port})
		slog.Info("tg dc override", "dc", o.ID, "addr", fmt.Sprintf("%s:%d", o.IP, o.Port))
	}
	list.Options = append(opts, list.Options...)
	return list
}

// slowRPCThreshold: TG calls slower than this are logged so stalls are visible.
const slowRPCThreshold = 3 * time.Second

// rpcLogger is a telegram middleware that surfaces failed and slow MTProto
// calls via slog. Without it a stuck upload.getFile / getMessages only ever
// shows up as a generic "context canceled" once the browser gives up — hiding
// flood-waits, DC migrations and connection stalls. Placed INSIDE the
// floodwait waiter so it observes the raw per-attempt result (incl. FLOOD_WAIT)
// before the waiter retries.
type rpcLogger struct{ sessionID int64 }

func (l rpcLogger) Handle(next tg.Invoker) telegram.InvokeFunc {
	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		start := time.Now()
		err := next.Invoke(ctx, input, output)
		elapsed := time.Since(start)
		switch {
		case err != nil && !errors.Is(err, context.Canceled):
			slog.Warn("tg rpc failed",
				"session_id", l.sessionID,
				"method", fmt.Sprintf("%T", input),
				"elapsed_ms", elapsed.Milliseconds(),
				"err", err)
		case elapsed > slowRPCThreshold:
			slog.Info("tg rpc slow",
				"session_id", l.sessionID,
				"method", fmt.Sprintf("%T", input),
				"elapsed_ms", elapsed.Milliseconds())
		}
		return err
	}
}

// Manager owns one persistent gotd client per active tg_session row. A user
// may bind multiple TG accounts; each gets its own client keyed by session id.
type Manager struct {
	cfg *config.Config
	db  *db.DB

	mu      sync.RWMutex
	clients map[int64]*entry // key: tg_session_id
}

type entry struct {
	userID int64
	client *telegram.Client
	api    *tg.Client
	stop   bg.StopFunc
}

// Client bundles the high-level + low-level handles a caller needs, plus the
// owning user id so callers can scope DB writes correctly.
type Client struct {
	UserID    int64
	SessionID int64
	Telegram  *telegram.Client
	API       *tg.Client
}

// waiterClient adapts (*telegram.Client + *floodwait.Waiter) to bg.Client so
// the waiter is running for the full lifetime of the underlying client.
type waiterClient struct {
	client *telegram.Client
	waiter *floodwait.Waiter
}

func (w waiterClient) Run(ctx context.Context, f func(ctx context.Context) error) error {
	return w.waiter.Run(ctx, func(ctx context.Context) error {
		return w.client.Run(ctx, f)
	})
}

func New(cfg *config.Config, database *db.DB) *Manager {
	return &Manager{
		cfg:     cfg,
		db:      database,
		clients: map[int64]*entry{},
	}
}

// RestoreActive starts a client for every active tg_session in DB.
// Failures are logged but do not abort startup.
func (m *Manager) RestoreActive(ctx context.Context) error {
	refs, err := m.db.ListActiveTGSessions(ctx)
	if err != nil {
		return fmt.Errorf("list active sessions: %w", err)
	}
	for _, ref := range refs {
		if err := m.Start(ctx, ref.UserID, ref.ID); err != nil {
			slog.Warn("restore tg client failed", "user_id", ref.UserID, "session_id", ref.ID, "err", err)
		}
	}
	slog.Info("tgmanager restored", "count", len(refs))
	return nil
}

// Start brings up a client for the given session (idempotent).
func (m *Manager) Start(ctx context.Context, userID, sessionID int64) error {
	m.mu.Lock()
	if _, ok := m.clients[sessionID]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	storage := &tgsession.Storage{DB: m.db, SessionID: sessionID, MasterKey: m.cfg.MasterKey}

	waiter := floodwait.NewWaiter().WithMaxRetries(5)

	// Middleware order is outermost→innermost. recovery reconnects on
	// connection-level failures (IO timeouts from DC migration); retry absorbs
	// transient server errors; the waiter backs off on FLOOD_WAIT; rpcLogger
	// observes the raw per-attempt result.
	client := telegram.NewClient(m.cfg.TgAPIID, m.cfg.TgAPIHash, telegram.Options{
		SessionStorage: storage,
		DCList:         DCList(m.cfg),
		Middlewares: []telegram.Middleware{
			tgmw.NewRecovery(ctx, recoveryBackoff),
			tgmw.NewRetry(5),
			waiter,
			rpcLogger{sessionID: sessionID},
		},
	})

	stop, err := bg.Connect(waiterClient{client: client, waiter: waiter})
	if err != nil {
		return fmt.Errorf("bg.Connect: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	if _, err := client.Self(probeCtx); err != nil {
		cancel()
		_ = stop()
		return fmt.Errorf("auth probe: %w", err)
	}
	cancel()

	m.mu.Lock()
	m.clients[sessionID] = &entry{
		userID: userID,
		client: client,
		api:    client.API(),
		stop:   stop,
	}
	m.mu.Unlock()

	slog.Info("tg client started", "user_id", userID, "session_id", sessionID)
	return nil
}

// Stop tears down the client for a session (no-op if not running).
func (m *Manager) Stop(sessionID int64) error {
	m.mu.Lock()
	e, ok := m.clients[sessionID]
	if ok {
		delete(m.clients, sessionID)
	}
	m.mu.Unlock()
	if !ok || e.stop == nil {
		return nil
	}
	return e.stop()
}

// ClientForSession returns the active client for a session.
func (m *Manager) ClientForSession(sessionID int64) (*Client, error) {
	m.mu.RLock()
	e, ok := m.clients[sessionID]
	m.mu.RUnlock()
	if !ok {
		return nil, errors.New("no telegram client for session")
	}
	return &Client{UserID: e.userID, SessionID: sessionID, Telegram: e.client, API: e.api}, nil
}

// ClientsForUser returns every running client owned by a user (for picking
// which one can serve a request — usually the request specifies sessionID,
// but legacy callers may iterate).
func (m *Manager) ClientsForUser(userID int64) []*Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Client
	for sid, e := range m.clients {
		if e.userID == userID {
			out = append(out, &Client{
				UserID: e.userID, SessionID: sid,
				Telegram: e.client, API: e.api,
			})
		}
	}
	return out
}

// Shutdown stops every client. Call before process exit.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for sid, e := range m.clients {
		if e.stop != nil {
			if err := e.stop(); err != nil {
				slog.Warn("stop client", "session_id", sid, "err", err)
			}
		}
	}
	m.clients = map[int64]*entry{}
}
