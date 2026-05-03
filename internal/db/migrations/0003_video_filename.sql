-- 重建 videos 表,字段命名严格对齐 TG Desktop 导出 JSON 的字段名。
-- 旧的 videos / favorites / cache_entries 数据全部丢弃 (用户明确要重构,
-- 后续靠 JSON 重新导入,所以不做迁移保留)。

DROP TABLE IF EXISTS favorites CASCADE;
DROP TABLE IF EXISTS cache_entries CASCADE;
DROP TABLE IF EXISTS videos CASCADE;

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE videos (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id  BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,

    -- 直接对应 JSON 字段
    tg_msg_id           BIGINT NOT NULL,           -- json: id
    msg_type            TEXT,                      -- json: type ("message" | "service")
    date                TIMESTAMPTZ,               -- json: date / date_unixtime
    edited              TIMESTAMPTZ,               -- json: edited / edited_unixtime
    from_name           TEXT,                      -- json: from
    from_id             TEXT,                      -- json: from_id (e.g. "channel2246296429")
    file                TEXT,                      -- json: file (often placeholder 文本)
    file_name           TEXT,                      -- json: file_name
    file_size           BIGINT,                    -- json: file_size
    thumbnail           TEXT,                      -- json: thumbnail
    thumbnail_file_size BIGINT,                    -- json: thumbnail_file_size
    media_type          TEXT,                      -- json: media_type
    mime_type           TEXT,                      -- json: mime_type
    duration_seconds    INT,                       -- json: duration_seconds
    width               INT,                       -- json: width
    height              INT,                       -- json: height
    text                TEXT,                      -- 由 JSON text 数组收敛为字符串
    text_entities       JSONB,                     -- json: text_entities 原样保留

    -- 流式播放需要的字段(JSON 没有,首次播放时从 TG 现取并回填)
    tg_doc_id      BIGINT,
    access_hash    BIGINT,
    file_reference BYTEA,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(user_id, channel_id, tg_msg_id)
);

-- 列表/排序: 按 user/channel + date 倒序
CREATE INDEX idx_videos_user_date    ON videos(user_id, date DESC NULLS LAST);
CREATE INDEX idx_videos_channel_date ON videos(channel_id, date DESC NULLS LAST);

-- ILIKE '%foo%' 走 trgm GIN 索引,中文友好且 100 万级也快
CREATE INDEX idx_videos_text_trgm     ON videos USING GIN (text gin_trgm_ops);
CREATE INDEX idx_videos_filename_trgm ON videos USING GIN (file_name gin_trgm_ops);

CREATE TABLE favorites (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    video_id   BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, video_id)
);
CREATE INDEX idx_favorites_user ON favorites(user_id, created_at DESC);

CREATE TABLE cache_entries (
    tg_doc_id        BIGINT PRIMARY KEY,
    file_path        TEXT NOT NULL,
    bytes            BIGINT NOT NULL DEFAULT 0,
    pinned           BOOLEAN NOT NULL DEFAULT FALSE,
    completed        BOOLEAN NOT NULL DEFAULT FALSE,
    last_accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_cache_lru ON cache_entries(pinned, last_accessed_at) WHERE completed = TRUE;
