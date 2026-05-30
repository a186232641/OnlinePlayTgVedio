package indexer

import (
	"context"
	"log/slog"
	"time"
)

// staggerDelay spaces out per-channel sync kickoffs within one sweep so we
// don't fire dozens of getHistory crawls against TG at the same instant.
const staggerDelay = 2 * time.Second

// StartScheduler launches a background goroutine that re-runs an incremental TG
// sync for every already-synced channel every `interval`. interval <= 0
// disables it (no goroutine started). The loop stops when ctx is cancelled.
func (i *Indexer) StartScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		slog.Info("auto-sync disabled (SYNC_INTERVAL=0)")
		return
	}
	go i.scheduleLoop(ctx, interval)
}

func (i *Indexer) scheduleLoop(ctx context.Context, interval time.Duration) {
	slog.Info("auto-sync scheduler started", "interval", interval.String())
	t := time.NewTicker(interval)
	defer t.Stop()
	// Wait for the first tick before the first sweep — by then RestoreActive has
	// brought TG clients online, so we don't waste a sweep on "client not ready".
	for {
		select {
		case <-ctx.Done():
			slog.Info("auto-sync scheduler stopped")
			return
		case <-t.C:
			i.runScheduledSweep(ctx)
		}
	}
}

// runScheduledSweep kicks off an incremental SyncStart for each eligible
// channel. SyncStart is idempotent (a channel already syncing is left alone)
// and incremental (only messages newer than what's stored), so most steady-state
// sweeps do one cheap probe per channel and return "up to date".
func (i *Indexer) runScheduledSweep(ctx context.Context) {
	refs, err := i.db.ChannelsForAutoSync(ctx)
	if err != nil {
		slog.Warn("auto-sync: list channels failed", "err", err)
		return
	}
	started, skipped := 0, 0
	for _, ref := range refs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Errors here are almost always "no telegram client for session"
		// (session not connected) — expected and non-fatal, just skip.
		if _, err := i.SyncStart(ctx, ref.ChannelID, ref.UserID); err != nil {
			skipped++
			slog.Debug("auto-sync skip channel", "channel_id", ref.ChannelID, "err", err)
			continue
		}
		started++
		select {
		case <-ctx.Done():
			return
		case <-time.After(staggerDelay):
		}
	}
	slog.Info("auto-sync sweep done", "eligible", len(refs), "started", started, "skipped", skipped)
}