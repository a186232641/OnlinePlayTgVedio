package db

import (
	"context"
	"errors"
	"strings"
	"time"

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

// --- Favorites grouped by source ---

// FavoriteSource is one channel or topic row the user has favorited media
// from: how many of each kind, when the latest was added, and that latest item
// (the cover of the group's card).
type FavoriteSource struct {
	ChannelID       int64
	Videos          int64
	Photos          int64
	LastFavoritedAt time.Time
	CoverKind       string
	CoverID         int64
}

// FavoriteSources groups the user's favorites (both tables) by the channel row
// they came from — for a forum group that is the topic, since media is stored
// on the topic row. Newest-favorited group first. kind ("" = both) restricts
// which favorites count.
//
// No pagination: the result is one row per distinct source, which is bounded
// by how many channels/topics the user has favorited anything from.
func (d *DB) FavoriteSources(ctx context.Context, userID int64, kind string) ([]FavoriteSource, error) {
	var parts []string
	if kind != MediaKindPhoto {
		parts = append(parts, `
            SELECT v.channel_id, 'video' AS kind, v.id, f.created_at
            FROM favorites f JOIN videos v ON v.id = f.video_id
            WHERE f.user_id = $1`)
	}
	if kind != MediaKindVideo {
		parts = append(parts, `
            SELECT p.channel_id, 'photo' AS kind, p.id, f.created_at
            FROM photo_favorites f JOIN photos p ON p.id = f.photo_id
            WHERE f.user_id = $1`)
	}
	rows, err := d.Query(ctx, `
        WITH fav AS (`+strings.Join(parts, " UNION ALL ")+`),
        agg AS (
            SELECT channel_id,
                   COUNT(*) FILTER (WHERE kind = 'video') AS videos,
                   COUNT(*) FILTER (WHERE kind = 'photo') AS photos,
                   MAX(created_at) AS last_at
            FROM fav GROUP BY channel_id
        ),
        cover AS (
            SELECT DISTINCT ON (channel_id) channel_id, kind, id
            FROM fav ORDER BY channel_id, created_at DESC, kind, id DESC
        )
        SELECT a.channel_id, a.videos, a.photos, a.last_at, c.kind, c.id
        FROM agg a JOIN cover c ON c.channel_id = a.channel_id
        ORDER BY a.last_at DESC, a.channel_id DESC
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FavoriteSource
	for rows.Next() {
		var s FavoriteSource
		if err := rows.Scan(&s.ChannelID, &s.Videos, &s.Photos, &s.LastFavoritedAt, &s.CoverKind, &s.CoverID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
