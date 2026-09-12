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

// --- Photo favorites ---
//
// Images live in their own table (see migration 0010) and the favorites table's
// video_id FK can't hold a photo id, so photo favorites get a parallel table.
// The API layer merges the two lists.

func (d *DB) AddPhotoFavorite(ctx context.Context, userID, photoID int64) error {
	_, err := d.Exec(ctx, `
        INSERT INTO photo_favorites (user_id, photo_id) VALUES ($1, $2)
        ON CONFLICT (user_id, photo_id) DO NOTHING
    `, userID, photoID)
	return err
}

func (d *DB) RemovePhotoFavorite(ctx context.Context, userID, photoID int64) error {
	_, err := d.Exec(ctx, `DELETE FROM photo_favorites WHERE user_id=$1 AND photo_id=$2`, userID, photoID)
	return err
}

func (d *DB) IsPhotoFavorite(ctx context.Context, userID, photoID int64) (bool, error) {
	var n int
	err := d.QueryRow(ctx, `SELECT 1 FROM photo_favorites WHERE user_id=$1 AND photo_id=$2`, userID, photoID).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
