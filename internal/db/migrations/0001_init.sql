CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE IF NOT EXISTS users (
    id            BIGSERIAL PRIMARY KEY,
    email         CITEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tg_sessions (
    user_id           BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    encrypted_session BYTEA,
    phone             TEXT,
    tg_user_id        BIGINT,
    status            TEXT NOT NULL DEFAULT 'pending',
    last_used_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS channels (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tg_channel_id   BIGINT NOT NULL,
    access_hash     BIGINT NOT NULL,
    title           TEXT NOT NULL,
    username        TEXT,
    photo_path      TEXT,
    video_count     INT NOT NULL DEFAULT 0,
    last_indexed_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, tg_channel_id)
);

CREATE INDEX IF NOT EXISTS idx_channels_user ON channels(user_id);

CREATE TABLE IF NOT EXISTS videos (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id      BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    tg_message_id   BIGINT NOT NULL,
    tg_doc_id       BIGINT NOT NULL,
    access_hash     BIGINT NOT NULL,
    file_reference  BYTEA NOT NULL,
    mime            TEXT,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    duration_sec    INT NOT NULL DEFAULT 0,
    width           INT NOT NULL DEFAULT 0,
    height          INT NOT NULL DEFAULT 0,
    caption         TEXT,
    sent_at         TIMESTAMPTZ,
    thumb_path      TEXT,
    caption_tsv     tsvector,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, channel_id, tg_message_id)
);

CREATE INDEX IF NOT EXISTS idx_videos_channel_sent ON videos(channel_id, sent_at DESC);
CREATE INDEX IF NOT EXISTS idx_videos_user_sent ON videos(user_id, sent_at DESC);
CREATE INDEX IF NOT EXISTS idx_videos_doc ON videos(tg_doc_id);
CREATE INDEX IF NOT EXISTS idx_videos_caption_tsv ON videos USING GIN(caption_tsv);

CREATE OR REPLACE FUNCTION videos_caption_tsv_update() RETURNS trigger AS $$
BEGIN
    NEW.caption_tsv := to_tsvector('simple', coalesce(NEW.caption, ''));
    RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_videos_caption_tsv ON videos;
CREATE TRIGGER trg_videos_caption_tsv
BEFORE INSERT OR UPDATE OF caption ON videos
FOR EACH ROW EXECUTE FUNCTION videos_caption_tsv_update();

CREATE TABLE IF NOT EXISTS favorites (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    video_id   BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, video_id)
);

CREATE INDEX IF NOT EXISTS idx_favorites_user ON favorites(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cache_entries (
    tg_doc_id        BIGINT PRIMARY KEY,
    file_path        TEXT NOT NULL,
    bytes            BIGINT NOT NULL DEFAULT 0,
    pinned           BOOLEAN NOT NULL DEFAULT FALSE,
    completed        BOOLEAN NOT NULL DEFAULT FALSE,
    last_accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cache_lru ON cache_entries(pinned, last_accessed_at) WHERE completed = TRUE;

CREATE TABLE IF NOT EXISTS index_jobs (
    user_id      BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'idle',
    channels_total   INT NOT NULL DEFAULT 0,
    channels_done    INT NOT NULL DEFAULT 0,
    videos_found     INT NOT NULL DEFAULT 0,
    last_error   TEXT,
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
