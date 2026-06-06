// Package dcpool keeps a persistent connection pool per Telegram data center on
// top of one authenticated *telegram.Client. Files live on a specific DC; using
// the right-DC pool avoids the repeated DC migration that makes a single client
// hit IO timeouts. Invokers are created lazily per DC and reused.
package dcpool

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
)

// Pool hands out tg.Clients bound to a specific DC's connection pool.
type Pool interface {
	// Client returns a tg.Client whose requests go to the given DC. A zero/
	// negative dc, or a creation failure, falls back to the default client.
	Client(dc int) *tg.Client
	Close() error
}

type pool struct {
	// baseCtx is used to establish DC connections (NOT a request ctx, which
	// could be cancelled mid-connect and abort the long-lived connection).
	baseCtx     context.Context
	api         *telegram.Client
	size        int64
	middlewares []telegram.Middleware

	mu       sync.Mutex
	invokers map[int]tg.Invoker
	closes   map[int]func() error
}

// NewPool builds a pool over an already-connected client. size is the max
// connections opened per DC. The middlewares are chained onto every per-DC
// invoker (the high-level client's middlewares don't apply to raw pool invokers).
func NewPool(ctx context.Context, c *telegram.Client, size int64, middlewares ...telegram.Middleware) Pool {
	return &pool{
		baseCtx:     ctx,
		api:         c,
		size:        size,
		middlewares: middlewares,
		invokers:    make(map[int]tg.Invoker),
		closes:      make(map[int]func() error),
	}
}

func (p *pool) current() int { return p.api.Config().ThisDC }

func (p *pool) Client(dc int) *tg.Client {
	if dc <= 0 {
		return tg.NewClient(p.api)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return tg.NewClient(p.invoker(dc))
}

// invoker lazily creates (or returns the cached) chained invoker for dc.
// Caller holds p.mu.
func (p *pool) invoker(dc int) tg.Invoker {
	if i, ok := p.invokers[dc]; ok {
		return i
	}

	var (
		inv telegram.CloseInvoker
		err error
	)
	if dc == p.current() {
		// Can't transfer to the current DC; use a plain pool on this client.
		inv, err = p.api.Pool(p.size)
	} else {
		inv, err = p.api.DC(p.baseCtx, dc, p.size)
	}
	if err != nil {
		slog.Warn("dcpool: create invoker failed, using default client", "dc", dc, "err", err)
		return p.api // degraded: *telegram.Client is itself a tg.Invoker
	}

	p.closes[dc] = inv.Close
	p.invokers[dc] = chainMiddlewares(inv, p.middlewares...)
	return p.invokers[dc]
}

func (p *pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	for _, c := range p.closes {
		if err := c(); err != nil {
			errs = append(errs, err)
		}
	}
	p.invokers = make(map[int]tg.Invoker)
	p.closes = make(map[int]func() error)
	return errors.Join(errs...)
}

// chainMiddlewares wraps invoker so chain[0] is outermost (first to run).
func chainMiddlewares(invoker tg.Invoker, chain ...telegram.Middleware) tg.Invoker {
	for i := len(chain) - 1; i >= 0; i-- {
		invoker = chain[i].Handle(invoker)
	}
	return invoker
}
