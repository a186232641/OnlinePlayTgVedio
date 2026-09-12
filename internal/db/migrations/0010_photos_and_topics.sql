-- 群组话题 (forum topics) + 图片 (photos) 支持。
--
-- 1) photos 单独建表而不是塞进 videos:图片的 locator 是 InputPhotoFileLocation
--    (photo id / access_hash / file_reference + 尺寸 type),和 document 是两个
--    id 空间,并且没有时长/mime。列名继续对齐 TG Desktop 导出 JSON,和 videos
--    保持同一套命名习惯。
-- 2) photo_favorites: favorites 表的 video_id 带 videos 外键,无法复用,图片收藏
--    单独一张表,前端/API 层合并两者。
-- 3) cache_entries 加 kind: 磁盘缓存现在同时存视频和图片。photo id 与 document
--    id 属于不同 id 空间,理论上可以撞号,必须和 kind 一起做主键。
-- 4) thumb_size / thumb_path: 缩略图按需从 TG 拉取后落盘。thumb_size 是同步时
--    从 Document.Thumbs / Photo.Sizes 里挑好的尺寸 type,请求缩略图时直接拿它
--    构造 location,省掉一次 getMessages。
-- 5) channels.is_forum: 论坛群组本质仍是 megagroup(dialog_kind 不变,列表页过滤
--    逻辑不受影响),只是额外打一个标记表示"它有话题,可以下钻"。话题本身是
--    dialog_kind='topic' 的子行,复用 0002 就建好的 parent_channel_id/topic_id。

CREATE TABLE photos (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id  BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,

    tg_msg_id     BIGINT NOT NULL,
    msg_type      TEXT,
    date          TIMESTAMPTZ,
    edited        TIMESTAMPTZ,
    from_name     TEXT,
    from_id       TEXT,
    file          TEXT,          -- json: photo ("photos/xxx.jpg"),仅 JSON 导入有值
    file_name     TEXT,          -- 图片通常没有文件名,保留列让搜索/排序与 videos 同构
    file_size     BIGINT,        -- 选中的最大尺寸的字节数
    width         INT,
    height        INT,
    text          TEXT,
    text_entities JSONB,
    grouped_id    BIGINT,        -- 相册 id,NULL = 非相册

    -- TG locator(JSON 导入的行为 NULL/0,首次查看时现取并回填)
    tg_photo_id    BIGINT,
    access_hash    BIGINT,
    file_reference BYTEA,
    dc_id          INT NOT NULL DEFAULT 0,
    size_type      TEXT,         -- 原图尺寸 type ('y'/'x'/...)
    thumb_size     TEXT,         -- 缩略图尺寸 type ('m'/'s'/...)
    thumb_path     TEXT,         -- 已落盘的缩略图,相对 CACHE_DIR

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(user_id, channel_id, tg_msg_id)
);

CREATE INDEX idx_photos_user_date    ON photos(user_id, date DESC NULLS LAST);
CREATE INDEX idx_photos_channel_date ON photos(channel_id, date DESC NULLS LAST);
CREATE INDEX idx_photos_text_trgm    ON photos USING GIN (text gin_trgm_ops);
CREATE INDEX idx_photos_grouped      ON photos (user_id, channel_id, grouped_id)
    WHERE grouped_id IS NOT NULL;

CREATE TABLE photo_favorites (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    photo_id   BIGINT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, photo_id)
);
CREATE INDEX idx_photo_favorites_user ON photo_favorites(user_id, created_at DESC);

-- 视频也需要缩略图,让两种卡片视觉统一。
ALTER TABLE videos ADD COLUMN thumb_size TEXT;
ALTER TABLE videos ADD COLUMN thumb_path TEXT;

-- 缓存条目按 (kind, id) 唯一。已有数据全是视频,默认 'video' 即可。
ALTER TABLE cache_entries ADD COLUMN kind TEXT NOT NULL DEFAULT 'video';
ALTER TABLE cache_entries DROP CONSTRAINT cache_entries_pkey;
ALTER TABLE cache_entries ADD PRIMARY KEY (kind, tg_doc_id);

-- 频道/话题元数据。
ALTER TABLE channels ADD COLUMN is_forum BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE channels ADD COLUMN photo_count INT NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN topics_synced_at TIMESTAMPTZ;
ALTER TABLE channels ADD COLUMN topic_icon_color INT;
ALTER TABLE channels ADD COLUMN topic_icon_emoji_id BIGINT;
ALTER TABLE channels ADD COLUMN topic_closed BOOLEAN NOT NULL DEFAULT FALSE;

-- 话题列表按父群组查,并按最后活跃排序。
CREATE INDEX idx_channels_parent ON channels(parent_channel_id) WHERE parent_channel_id IS NOT NULL;
