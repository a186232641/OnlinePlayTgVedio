-- One user may now bind many TG sessions; each tg_sessions row gets its own
-- BIGSERIAL id. The old behavior (PK on user_id) limited each user to one
-- session.
ALTER TABLE tg_sessions ADD COLUMN id BIGSERIAL;
ALTER TABLE tg_sessions ADD COLUMN label TEXT;
ALTER TABLE tg_sessions ADD COLUMN discover_status TEXT NOT NULL DEFAULT 'idle';
ALTER TABLE tg_sessions ADD COLUMN discover_started_at TIMESTAMPTZ;
ALTER TABLE tg_sessions ADD COLUMN discover_finished_at TIMESTAMPTZ;
ALTER TABLE tg_sessions ADD COLUMN discover_error TEXT;
ALTER TABLE tg_sessions DROP CONSTRAINT tg_sessions_pkey;
ALTER TABLE tg_sessions ADD PRIMARY KEY (id);
CREATE INDEX IF NOT EXISTS idx_tg_sessions_user ON tg_sessions(user_id);

-- channels gains: link to its session, dialog kind, forum-topic columns,
-- per-channel index toggle + status. parent_channel_id is a self-FK so
-- forum topics can be modeled as child rows of the parent forum group.
ALTER TABLE channels ADD COLUMN tg_session_id BIGINT REFERENCES tg_sessions(id) ON DELETE CASCADE;
ALTER TABLE channels ADD COLUMN dialog_kind TEXT NOT NULL DEFAULT 'channel';
ALTER TABLE channels ADD COLUMN parent_channel_id BIGINT REFERENCES channels(id) ON DELETE CASCADE;
ALTER TABLE channels ADD COLUMN topic_id INTEGER;
ALTER TABLE channels ADD COLUMN index_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE channels ADD COLUMN index_status TEXT NOT NULL DEFAULT 'idle';
ALTER TABLE channels ADD COLUMN index_error TEXT;

-- backfill: each existing user has at most one session, so we can map by user_id
UPDATE channels c SET tg_session_id = s.id FROM tg_sessions s WHERE s.user_id = c.user_id;

-- preserve current behavior: anything already indexed stays enabled
UPDATE channels SET index_enabled = TRUE WHERE last_indexed_at IS NOT NULL;

-- the same tg_channel_id can now host many topic rows; uniqueness includes topic_id
ALTER TABLE channels DROP CONSTRAINT channels_user_id_tg_channel_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_channels_session_chan_topic
    ON channels(tg_session_id, tg_channel_id, COALESCE(topic_id, 0));

-- index_jobs was a single per-user "scan everything" job; status is now
-- per-channel (channels.index_status) and per-session (tg_sessions.discover_status).
DROP TABLE IF EXISTS index_jobs;