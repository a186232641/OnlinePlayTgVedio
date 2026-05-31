-- Telegram albums (multiple videos sent together) are multiple messages
-- sharing one grouped_id, and only one of them carries the caption. Without
-- the group id we can't tell siblings apart, so a text search only ever finds
-- the captioned message. Store grouped_id so sync can propagate the album's
-- caption to every member's `text` (making them all searchable/displayable).
ALTER TABLE videos ADD COLUMN grouped_id BIGINT;

-- Lookups during caption propagation are by (user_id, channel_id, grouped_id).
CREATE INDEX IF NOT EXISTS idx_videos_grouped
    ON videos (user_id, channel_id, grouped_id)
    WHERE grouped_id IS NOT NULL;