package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Collections are the user's own named groups of videos and images — any mix
// of streamers, channels and topics. They are independent of favorites: adding
// to one neither favorites the item nor pins it in the disk cache.
//
// Membership lives in collection_videos / collection_photos (two id spaces,
// like favorites / photo_favorites). Listing a collection's media goes through
// ListMedia with CollectionID, which joins those tables exactly the way a
// favorites listing joins its own.

var ErrCollectionNotFound = errors.New("collection not found")

// Collection is one group with its membership summary: counts per kind, the
// latest addition (LastAddedAt) and that item as the cover. An empty group has
// no LastAddedAt and no cover.
type Collection struct {
	ID          int64
	Name        string
	CreatedAt   time.Time
	Videos      int64
	Photos      int64
	LastAddedAt *time.Time
	CoverKind   string
	CoverID     int64
}

func (d *DB) CreateCollection(ctx context.Context, userID int64, name string) (*Collection, error) {
	c := &Collection{Name: name}
	err := d.QueryRow(ctx, `
        INSERT INTO collections (user_id, name) VALUES ($1, $2)
        RETURNING id, created_at
    `, userID, name).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d *DB) RenameCollection(ctx context.Context, userID, id int64, name string) error {
	tag, err := d.Exec(ctx, `UPDATE collections SET name=$3 WHERE id=$1 AND user_id=$2`, id, userID, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCollectionNotFound
	}
	return nil
}

// DeleteCollection drops the group and (by cascade) its membership rows; the
// media themselves are untouched.
func (d *DB) DeleteCollection(ctx context.Context, userID, id int64) error {
	tag, err := d.Exec(ctx, `DELETE FROM collections WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCollectionNotFound
	}
	return nil
}

// collectionSummarySQL lists the user's collections with their membership
// summary, most recently used first (an empty group by its creation time).
// $1 = user id.
const collectionSummarySQL = `
    WITH member AS (
        SELECT cv.collection_id, 'video' AS kind, cv.video_id AS id, cv.created_at
        FROM collection_videos cv JOIN collections c ON c.id = cv.collection_id
        WHERE c.user_id = $1
        UNION ALL
        SELECT cp.collection_id, 'photo', cp.photo_id, cp.created_at
        FROM collection_photos cp JOIN collections c ON c.id = cp.collection_id
        WHERE c.user_id = $1
    ),
    agg AS (
        SELECT collection_id,
               COUNT(*) FILTER (WHERE kind = 'video') AS videos,
               COUNT(*) FILTER (WHERE kind = 'photo') AS photos,
               MAX(created_at) AS last_at
        FROM member GROUP BY collection_id
    ),
    cover AS (
        SELECT DISTINCT ON (collection_id) collection_id, kind, id
        FROM member ORDER BY collection_id, created_at DESC, kind, id DESC
    )
    SELECT c.id, c.name, c.created_at,
           COALESCE(a.videos, 0), COALESCE(a.photos, 0), a.last_at,
           COALESCE(cv.kind, ''), COALESCE(cv.id, 0)
    FROM collections c
    LEFT JOIN agg a ON a.collection_id = c.id
    LEFT JOIN cover cv ON cv.collection_id = c.id
    WHERE c.user_id = $1`

func scanCollection(row pgx.Row) (*Collection, error) {
	var c Collection
	err := row.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.Videos, &c.Photos, &c.LastAddedAt, &c.CoverKind, &c.CoverID)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (d *DB) ListCollections(ctx context.Context, userID int64) ([]Collection, error) {
	rows, err := d.Query(ctx, collectionSummarySQL+`
        ORDER BY COALESCE(a.last_at, c.created_at) DESC, c.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// CollectionByID returns one of the user's collections, or
// ErrCollectionNotFound — which is also the ownership check for every
// per-collection endpoint.
func (d *DB) CollectionByID(ctx context.Context, userID, id int64) (*Collection, error) {
	c, err := scanCollection(d.QueryRow(ctx, collectionSummarySQL+` AND c.id = $2`, userID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCollectionNotFound
	}
	return c, err
}

// collectionMemberTable maps a media kind onto its membership table and column.
func collectionMemberTable(kind string) (table, fk string) {
	if kind == MediaKindPhoto {
		return "collection_photos", "photo_id"
	}
	return "collection_videos", "video_id"
}

// AddToCollection adds one media row. The caller has checked that both the
// collection and the media row belong to the user. Adding twice is a no-op.
func (d *DB) AddToCollection(ctx context.Context, collectionID int64, kind string, mediaID int64) error {
	table, fk := collectionMemberTable(kind)
	_, err := d.Exec(ctx, `
        INSERT INTO `+table+` (collection_id, `+fk+`) VALUES ($1, $2)
        ON CONFLICT DO NOTHING
    `, collectionID, mediaID)
	return err
}

// RemoveFromCollection removes one media row; the collection must be the
// user's (checked in the same statement).
func (d *DB) RemoveFromCollection(ctx context.Context, userID, collectionID int64, kind string, mediaID int64) error {
	table, fk := collectionMemberTable(kind)
	_, err := d.Exec(ctx, `
        DELETE FROM `+table+`
        WHERE collection_id = $1 AND `+fk+` = $2
          AND collection_id IN (SELECT id FROM collections WHERE user_id = $3)
    `, collectionID, mediaID, userID)
	return err
}

// CollectionsContaining returns the ids of the user's collections that hold
// the given media row — what the "加入分组" picker ticks.
func (d *DB) CollectionsContaining(ctx context.Context, userID int64, kind string, mediaID int64) (map[int64]bool, error) {
	table, fk := collectionMemberTable(kind)
	rows, err := d.Query(ctx, `
        SELECT m.collection_id FROM `+table+` m
        JOIN collections c ON c.id = m.collection_id
        WHERE c.user_id = $1 AND m.`+fk+` = $2
    `, userID, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
