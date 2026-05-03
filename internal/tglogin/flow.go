package tglogin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgsession"
)

// Stage values describe where the login dialogue currently is.
type Stage string

const (
	StageInit         Stage = "init"
	StageCodeRequired Stage = "code_required"
	StagePassword     Stage = "password_required"
	StageDone         Stage = "done"
	StageError        Stage = "error"
)

const flowTTL = 15 * time.Minute

type pending struct {
	id        string
	userID    int64
	sessionID int64
	phone     string

	mu    sync.Mutex
	stage Stage
	err   error

	codeCh         chan string
	passwordCh     chan string
	codeRequiredCh chan struct{}
	passwordCh1    chan struct{} // closed when Password() is called
	doneCh         chan struct{} // closed when the whole flow ends (success or error)

	cancel    context.CancelFunc
	createdAt time.Time
}

func (p *pending) setStage(s Stage)       { p.mu.Lock(); p.stage = s; p.mu.Unlock() }
func (p *pending) currentStage() Stage    { p.mu.Lock(); defer p.mu.Unlock(); return p.stage }
func (p *pending) setError(e error)       { p.mu.Lock(); p.err = e; p.stage = StageError; p.mu.Unlock() }
func (p *pending) currentError() error    { p.mu.Lock(); defer p.mu.Unlock(); return p.err }

// Manager owns all in-flight TG login flows.
type Manager struct {
	cfg *config.Config
	db  *db.DB

	mu    sync.Mutex
	flows map[string]*pending

	// onActivated is invoked after a session goes active so the caller
	// (e.g. tgmanager) can spin up the persistent client for it.
	onActivated func(userID, sessionID int64)
}

func NewManager(cfg *config.Config, database *db.DB, onActivated func(userID, sessionID int64)) *Manager {
	m := &Manager{cfg: cfg, db: database, flows: map[string]*pending{}, onActivated: onActivated}
	go m.gcLoop()
	return m
}

func (m *Manager) gcLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		m.mu.Lock()
		for id, p := range m.flows {
			if now.Sub(p.createdAt) > flowTTL {
				if p.cancel != nil {
					p.cancel()
				}
				delete(m.flows, id)
			}
		}
		m.mu.Unlock()
	}
}

// Start begins a new flow for the given user and phone. It returns once
// SendCode has fired (i.e. the SMS has been dispatched) or with an error.
func (m *Manager) Start(parentCtx context.Context, userID int64, phone string) (string, Stage, error) {
	if phone == "" {
		return "", StageError, errors.New("phone required")
	}

	id, err := genID()
	if err != nil {
		return "", StageError, err
	}

	// Create a fresh tg_sessions row up-front so the gotd session storage
	// has a stable row to write the encrypted blob into. Multiple binds
	// for the same user thus get distinct rows.
	sessionID, err := m.db.CreateTGSession(parentCtx, userID, phone)
	if err != nil {
		return "", StageError, fmt.Errorf("create tg_session: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &pending{
		id:             id,
		userID:         userID,
		sessionID:      sessionID,
		phone:          phone,
		stage:          StageInit,
		codeCh:         make(chan string, 1),
		passwordCh:     make(chan string, 1),
		codeRequiredCh: make(chan struct{}),
		passwordCh1:    make(chan struct{}),
		doneCh:         make(chan struct{}),
		cancel:         cancel,
		createdAt:      time.Now(),
	}

	m.mu.Lock()
	m.flows[id] = p
	m.mu.Unlock()

	storage := &tgsession.Storage{DB: m.db, SessionID: sessionID, MasterKey: m.cfg.MasterKey}
	client := telegram.NewClient(m.cfg.TgAPIID, m.cfg.TgAPIHash, telegram.Options{
		SessionStorage: storage,
	})

	// Drive the flow in a goroutine. We do NOT inherit parentCtx because the
	// HTTP request that started the flow returns long before the flow ends.
	go func() {
		defer close(p.doneCh)
		defer cancel()

		err := client.Run(ctx, func(rctx context.Context) error {
			a := &authenticator{p: p}
			flow := auth.NewFlow(a, auth.SendCodeOptions{})
			if err := flow.Run(rctx, client.Auth()); err != nil {
				return fmt.Errorf("auth flow: %w", err)
			}
			self, err := client.Self(rctx)
			if err != nil {
				return fmt.Errorf("self: %w", err)
			}
			if err := m.db.MarkTGSessionActive(rctx, p.sessionID, p.phone, self.ID); err != nil {
				return fmt.Errorf("mark active: %w", err)
			}
			return nil
		})
		if err != nil {
			slog.Warn("tg login failed", "user_id", userID, "session_id", p.sessionID, "err", err)
			p.setError(err)
			return
		}
		p.setStage(StageDone)
		if m.onActivated != nil {
			m.onActivated(userID, p.sessionID)
		}
	}()

	// Wait for the flow to either reach "code required" or fail.
	select {
	case <-p.codeRequiredCh:
		p.setStage(StageCodeRequired)
		return id, StageCodeRequired, nil
	case <-p.doneCh:
		if e := p.currentError(); e != nil {
			m.deleteFlow(id)
			return "", StageError, e
		}
		return id, StageDone, nil
	case <-parentCtx.Done():
		return id, p.currentStage(), parentCtx.Err()
	}
}

// SubmitCode delivers the SMS code to the running authenticator.
// It blocks until the flow reaches the next decision point.
func (m *Manager) SubmitCode(parentCtx context.Context, id, code string) (Stage, error) {
	p, ok := m.get(id)
	if !ok {
		return StageError, errors.New("flow not found")
	}
	select {
	case p.codeCh <- code:
	case <-time.After(5 * time.Second):
		return p.currentStage(), errors.New("flow not waiting for code")
	}

	select {
	case <-p.passwordCh1:
		p.setStage(StagePassword)
		return StagePassword, nil
	case <-p.doneCh:
		if e := p.currentError(); e != nil {
			m.deleteFlow(id)
			return StageError, e
		}
		m.deleteFlow(id)
		return StageDone, nil
	case <-time.After(30 * time.Second):
		return p.currentStage(), errors.New("timed out waiting for next step")
	case <-parentCtx.Done():
		return p.currentStage(), parentCtx.Err()
	}
}

// SubmitPassword delivers the 2FA password.
func (m *Manager) SubmitPassword(parentCtx context.Context, id, password string) (Stage, error) {
	p, ok := m.get(id)
	if !ok {
		return StageError, errors.New("flow not found")
	}
	select {
	case p.passwordCh <- password:
	case <-time.After(5 * time.Second):
		return p.currentStage(), errors.New("flow not waiting for password")
	}

	select {
	case <-p.doneCh:
		if e := p.currentError(); e != nil {
			m.deleteFlow(id)
			return StageError, e
		}
		m.deleteFlow(id)
		return StageDone, nil
	case <-time.After(30 * time.Second):
		return p.currentStage(), errors.New("timed out waiting for completion")
	case <-parentCtx.Done():
		return p.currentStage(), parentCtx.Err()
	}
}

func (m *Manager) get(id string) (*pending, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.flows[id]
	return p, ok
}

func (m *Manager) deleteFlow(id string) {
	m.mu.Lock()
	delete(m.flows, id)
	m.mu.Unlock()
}

// authenticator is the gotd auth.UserAuthenticator that drains channels.
type authenticator struct {
	p           *pending
	codeOnce    sync.Once
	pwOnce      sync.Once
}

func (a *authenticator) Phone(ctx context.Context) (string, error) {
	return a.p.phone, nil
}

func (a *authenticator) Code(ctx context.Context, _ *tg.AuthSentCode) (string, error) {
	a.codeOnce.Do(func() { close(a.p.codeRequiredCh) })
	select {
	case c := <-a.p.codeCh:
		return c, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (a *authenticator) Password(ctx context.Context) (string, error) {
	a.pwOnce.Do(func() { close(a.p.passwordCh1) })
	select {
	case p := <-a.p.passwordCh:
		return p, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (a *authenticator) AcceptTermsOfService(ctx context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func (a *authenticator) SignUp(ctx context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up via TG client not supported here")
}

func genID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
