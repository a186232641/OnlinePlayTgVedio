-- Per-channel opt-out for the background auto-sync scheduler. The manual
-- "TG 同步" button always works regardless; this flag only controls whether the
-- periodic sweep includes the channel. Defaults TRUE so existing behavior
-- (every once-synced channel auto-updates) is preserved.
ALTER TABLE channels ADD COLUMN auto_sync BOOLEAN NOT NULL DEFAULT TRUE;
