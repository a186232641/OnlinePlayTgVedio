// Package tgmw holds gotd telegram client middlewares used across the manager
// and the per-DC connection pool: transient-error retry and connection recovery.
package tgmw

import (
	"context"
	"fmt"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// transientErrors are server-side hiccups worth retrying. They are distinct
// from FLOOD_WAIT (handled by the floodwait waiter) and from real business
// errors (which must propagate). Mirrors tdl's list.
var transientErrors = []string{
	"Timedout",
	"No workers running",
	"RPC_CALL_FAIL",
	"RPC_MCGET_FAIL",
	"WORKER_BUSY_TOO_LONG_RETRY",
	"memory limit exit",
}

type retry struct {
	max  int
	errs []string
}

// NewRetry retries an RPC up to max times when it fails with one of the
// transient server errors above. Stateless and concurrency-safe.
func NewRetry(max int) telegram.Middleware {
	return retry{max: max, errs: transientErrors}
}

func (r retry) Handle(next tg.Invoker) telegram.InvokeFunc {
	return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
		var lastErr error
		for attempt := 0; attempt < r.max; attempt++ {
			err := next.Invoke(ctx, input, output)
			if err == nil {
				return nil
			}
			if !tgerr.Is(err, r.errs...) {
				return err
			}
			lastErr = err
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		return fmt.Errorf("retry limit reached after %d attempts: %w", r.max, lastErr)
	}
}
