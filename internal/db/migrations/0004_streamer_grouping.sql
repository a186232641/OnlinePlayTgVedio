-- Per-channel "group by streamer" feature.
--
-- Some channels post videos named "{streamer}-YYYY-MM-DD HH:MM:SS.ext". We
-- extract the streamer prefix into a STORED generated column so:
--   * existing rows are populated automatically (no backfill script),
--   * future inserts (JSON import + TG sync) stay in sync with zero app code,
--   * grouping/filtering is index-backed.
-- Filenames not matching the pattern yield NULL (bucketed as "其它" in the UI).

ALTER TABLE channels ADD COLUMN group_by_streamer BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE videos ADD COLUMN streamer TEXT
    GENERATED ALWAYS AS (substring(file_name from '^(.+?)-[0-9]{4}-[0-9]{2}-[0-9]{2}')) STORED;

CREATE INDEX idx_videos_channel_streamer ON videos(channel_id, streamer);