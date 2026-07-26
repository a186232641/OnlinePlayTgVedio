package tgmw

import (
	"context"
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type recovery struct {
	ctx        context.Context
	newBackoff func() backoff.BackOff
}

// NewRecovery reconnects (with exponential backoff) on any non-business,
// non-cancel error — i.e. connection-level failures such as the IO timeouts a
// single client hits when Telegram keeps migrating it between DCs. Telegram
// business errors (tgerr.*) and context cancellation are passed through
// untouched.
//
// newBackoff is invoked once per RPC so each call gets its own backoff state;
// this keeps the middleware concurrency-safe when shared across the many
// parallel invocations a download pool issues.
func NewRecovery(ctx context.Context, newBackoff func() backoff.BackOff) telegram.Middleware {
	return &recovery{ctx: ctx, newBackoff: newBackoff}
}

func (r *recovery) Handle(next tg.Invoker) telegram.InvokeFunc {
	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		return backoff.RetryNotify(func() error {
			err := next.Invoke(ctx, input, output)
			if err == nil {
				return nil
			}
			if r.shouldRecover(ctx, err) {
				return err // transient connection error → retry
			}
			return backoff.Permanent(err)
		}, r.newBackoff(), func(err error, d time.Duration) {
			slog.Debug("tg recovery wait", "err", err, "backoff_ms", d.Milliseconds())
		})
	}
}

func (r *recovery) shouldRecover(ctx context.Context, err error) bool {
	// r.ctx lets an external shutdown abort recovery; ctx is the per-call ctx
	// (cancelled when the browser disconnects or the download is aborted).
	select {
	case <-r.ctx.Done():
		return false
	case <-ctx.Done():
		return false
	default:
	}
	// Recover from anything that is NOT a Telegram business error.
	_, isBusiness := tgerr.As(err)
	return !isBusiness
}
