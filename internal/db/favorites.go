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

// --- Favorites grouped ---

// What FavoriteGroups groups by.
const (
	// FavGroupBySource: the channel row an item belongs to — for a forum group
	// that is the topic, since media is stored on the topic row.
	FavGroupBySource = "source"
	// FavGroupByStreamer: videos.streamer, the "{streamer}-YYYY-MM-DD" filename
	// prefix (migration 0004). Videos only — images have no such name — and
	// global rather than per channel: one streamer's recordings often land in
	// several channels. Unmatched filenames form the "" bucket.
	FavGroupByStreamer = "streamer"
)

// FavoriteGroup is one bucket of the user's favorites: how many of each kind,
// when the latest was added, and that latest item (the cover of its card).
type FavoriteGroup struct {
	// Key is the channel id (as text) for FavGroupBySource, the streamer name
	// for FavGroupByStreamer ("" = filenames without a streamer).
	Key             string
	Videos          int64
	Photos          int64
	LastFavoritedAt time.Time
	CoverKind       string
	CoverID         int64
}

// FavoriteGroups buckets the user's favorites (both tables) by `by`,
// newest-favorited bucket first. kind ("" = both) restricts which favorites
// count; grouping by streamer implies videos.
//
// No pagination: the result is one row per distinct bucket, bounded by how
// many channels/streamers the user has favorited anything from.
func (d *DB) FavoriteGroups(ctx context.Context, userID int64, by, kind string) ([]FavoriteGroup, error) {
	videoKey, photoKey := "v.channel_id::text", "p.channel_id::text"
	switch by {
	case FavGroupBySource:
	case FavGroupByStreamer:
		videoKey, kind = "COALESCE(v.streamer, '')", MediaKindVideo
	default:
		return nil, errors.New("unknown favorites grouping: " + by)
	}
	var parts []string
	if kind != MediaKindPhoto {
		parts = append(parts, `
            SELECT `+videoKey+` AS gkey, 'video' AS kind, v.id, f.created_at
            FROM favorites f JOIN videos v ON v.id = f.video_id
            WHERE f.user_id = $1`)
	}
	if kind != MediaKindVideo {
		parts = append(parts, `
            SELECT `+photoKey+` AS gkey, 'photo' AS kind, p.id, f.created_at
            FROM photo_favorites f JOIN photos p ON p.id = f.photo_id
            WHERE f.user_id = $1`)
	}
	rows, err := d.Query(ctx, `
        WITH fav AS (`+strings.Join(parts, " UNION ALL ")+`),
        agg AS (
            SELECT gkey,
                   COUNT(*) FILTER (WHERE kind = 'video') AS videos,
                   COUNT(*) FILTER (WHERE kind = 'photo') AS photos,
                   MAX(created_at) AS last_at
            FROM fav GROUP BY gkey
        ),
        cover AS (
            SELECT DISTINCT ON (gkey) gkey, kind, id
            FROM fav ORDER BY gkey, created_at DESC, kind, id DESC
        )
        SELECT a.gkey, a.videos, a.photos, a.last_at, c.kind, c.id
        FROM agg a JOIN cover c ON c.gkey = a.gkey
        ORDER BY a.last_at DESC, a.gkey
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FavoriteGroup
	for rows.Next() {
		var g FavoriteGroup
		if err := rows.Scan(&g.Key, &g.Videos, &g.Photos, &g.LastFavoritedAt, &g.CoverKind, &g.CoverID); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
