-- 用户自建分组(收藏夹):把任意视频/图片——不同主播、不同话题——归到一个命名分组里。
--
-- 与收藏相互独立:加入分组不会收藏,也不固定磁盘缓存。
-- 成员表按 videos / photos 拆成两张,原因同 favorites / photo_favorites:两边是
-- 不同的 id 空间,各自带外键。created_at 是加入分组的时间,列表按它排序分页
-- (与收藏的 f.created_at 同一套查询)。

CREATE TABLE collections (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);

CREATE TABLE collection_videos (
    collection_id BIGINT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    video_id      BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (collection_id, video_id)
);
CREATE INDEX idx_collection_videos_added ON collection_videos(collection_id, created_at DESC);
CREATE INDEX idx_collection_videos_video ON collection_videos(video_id);

CREATE TABLE collection_photos (
    collection_id BIGINT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    photo_id      BIGINT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (collection_id, photo_id)
);
CREATE INDEX idx_collection_photos_added ON collection_photos(collection_id, created_at DESC);
CREATE INDEX idx_collection_photos_photo ON collection_photos(photo_id);
