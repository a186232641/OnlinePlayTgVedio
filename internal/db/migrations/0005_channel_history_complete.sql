-- Resumable sync needs to know whether a channel's history has been walked all
-- the way to its oldest message. Once true, sweeps skip the backfill pass and
-- only pull new messages at the top. Existing channels start false → the next
-- sync does one backfill pass to the true bottom, then flips this to true.
ALTER TABLE channels ADD COLUMN history_complete BOOLEAN NOT NULL DEFAULT FALSE;
