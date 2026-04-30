package db

import (
	"context"
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
		if err.Error() == "no rows in result set" {
			return false, nil
		}
		// pgx returns pgx.ErrNoRows; check loosely
		return false, nil
	}
	return true, nil
}
