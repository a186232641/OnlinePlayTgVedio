package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Channel struct {
	ID            int64
	UserID        int64
	TGChannelID   int64
	AccessHash    int64
	Title         string
	Username      string
	PhotoPath     string
	VideoCount    int
	LastIndexedAt *time.Time
}

func (d *DB) UpsertChannel(ctx context.Context, c *Channel) (int64, error) {
	row := d.QueryRow(ctx, `
        INSERT INTO channels (user_id, tg_channel_id, access_hash, title, username)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (user_id, tg_channel_id) DO UPDATE SET
            access_hash = EXCLUDED.access_hash,
            title       = EXCLUDED.title,
            username    = EXCLUDED.username
        RETURNING id
    `, c.UserID, c.TGChannelID, c.AccessHash, c.Title, nilIfEmpty(c.Username))
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) MarkChannelIndexed(ctx context.Context, channelID int64, videoCount int) error {
	_, err := d.Exec(ctx, `
        UPDATE channels SET video_count=$1, last_indexed_at=NOW() WHERE id=$2
    `, videoCount, channelID)
	return err
}

func (d *DB) ListChannels(ctx context.Context, userID int64) ([]Channel, error) {
	rows, err := d.Query(ctx, `
        SELECT id, user_id, tg_channel_id, access_hash, title,
               COALESCE(username, ''), COALESCE(photo_path, ''),
               video_count, last_indexed_at
        FROM channels WHERE user_id=$1
        ORDER BY last_indexed_at DESC NULLS LAST, title
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.UserID, &c.TGChannelID, &c.AccessHash, &c.Title,
			&c.Username, &c.PhotoPath, &c.VideoCount, &c.LastIndexedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) ChannelByID(ctx context.Context, id, userID int64) (*Channel, error) {
	row := d.QueryRow(ctx, `
        SELECT id, user_id, tg_channel_id, access_hash, title,
               COALESCE(username, ''), COALESCE(photo_path, ''),
               video_count, last_indexed_at
        FROM channels WHERE id=$1 AND user_id=$2
    `, id, userID)
	c := &Channel{}
	if err := row.Scan(&c.ID, &c.UserID, &c.TGChannelID, &c.AccessHash, &c.Title,
		&c.Username, &c.PhotoPath, &c.VideoCount, &c.LastIndexedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
