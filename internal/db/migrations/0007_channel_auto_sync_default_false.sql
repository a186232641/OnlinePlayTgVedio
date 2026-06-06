-- Flip the default for channels.auto_sync from TRUE to FALSE: newly
-- discovered/imported channels are NOT picked up by the background scheduler
-- unless the user explicitly turns auto-sync on. Existing rows keep whatever
-- value they already have.
ALTER TABLE channels ALTER COLUMN auto_sync SET DEFAULT FALSE;