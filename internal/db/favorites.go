package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (d *DB) AddFavorite(ctx context.Context, userID, videoID int64) error {
	_, err := d.Exec(ctx, `
        INSERT INTO favorites (user_id, video_id) VALUES ($1, $2)
        ON CONFLICT (user_id, video_id) DO NOTHING
    `, userID, videoID)
	return err
}

func (d *DB) RemoveFavorite(ctx context.Context, userID, videoID int64) error {
	_, err := d.Exec(ctx, `DELETE FROM favorites WHERE user_id=$1 AND video_id=$2`, userID, videoID)
	return err
}

func (d *DB) IsFavorite(ctx context.Context, userID, videoID int64) (bool, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT 1 FROM favorites WHERE user_id=$1 AND video_id=$2`, userID, videoID).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
