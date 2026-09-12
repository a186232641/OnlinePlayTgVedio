package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// CacheEntry is one file in the on-disk media cache.
//
// Kind + TGDocID is the identity: videos are keyed by their Telegram document
// id, images by their photo id, and those are two independent id spaces that
// could in principle collide. TGDocID keeps its name for the video case; for
// Kind == MediaKindPhoto it holds the photo id.
type CacheEntry struct {
	Kind           string
	TGDocID        int64
	FilePath       string
	Bytes          int64
	Pinned         bool
	Completed      bool
	LastAccessedAt time.Time
}

func (d *DB) GetCacheEntry(ctx context.Context, kind string, docID int64) (*CacheEntry, error) {
	row := d.QueryRow(ctx, `
        SELECT kind, tg_doc_id, file_path, bytes, pinned, completed
        FROM cache_entries WHERE kind=$1 AND tg_doc_id=$2
    `, kind, docID)
	c := &CacheEntry{}
	if err := row.Scan(&c.Kind, &c.TGDocID, &c.FilePath, &c.Bytes, &c.Pinned, &c.Completed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (d *DB) UpsertCacheEntry(ctx context.Context, c *CacheEntry) error {
	_, err := d.Exec(ctx, `
        INSERT INTO cache_entries (kind, tg_doc_id, file_path, bytes, pinned, completed, last_accessed_at)
        VALUES ($1, $2, $3, $4, $5, $6, NOW())
        ON CONFLICT (kind, tg_doc_id) DO UPDATE SET
            file_path = EXCLUDED.file_path, bytes = EXCLUDED.bytes,
            pinned = cache_entries.pinned OR EXCLUDED.pinned,
            completed = cache_entries.completed OR EXCLUDED.completed,
            last_accessed_at = NOW()
    `, c.Kind, c.TGDocID, c.FilePath, c.Bytes, c.Pinned, c.Completed)
	return err
}

// MarkCacheIncomplete resets an entry to not-completed (e.g. when the on-disk
// file failed an integrity check), so the next play re-downloads it. The pinned
// flag is preserved, so favorites stay pinned across the re-download.
func (d *DB) MarkCacheIncomplete(ctx context.Context, kind string, docID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE cache_entries SET completed=false, bytes=0 WHERE kind=$1 AND tg_doc_id=$2
    `, kind, docID)
	return err
}

func (d *DB) TouchCache(ctx context.Context, kind string, docID int64) error {
	_, err := d.Exec(ctx, `UPDATE cache_entries SET last_accessed_at=NOW() WHERE kind=$1 AND tg_doc_id=$2`, kind, docID)
	return err
}

// AllCompletedCacheEntries provides the full on-disk cache inventory, including
// LRU timestamps, for reconciliation and strict capacity enforcement. Spans
// every kind — eviction treats one shared byte budget.
func (d *DB) AllCompletedCacheEntries(ctx context.Context) ([]CacheEntry, error) {
	rows, err := d.Query(ctx, `
        SELECT kind, tg_doc_id, file_path, bytes, pinned, completed, last_accessed_at
        FROM cache_entries WHERE completed=true ORDER BY last_accessed_at ASC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CacheEntry
	for rows.Next() {
		var c CacheEntry
		if err := rows.Scan(&c.Kind, &c.TGDocID, &c.FilePath, &c.Bytes, &c.Pinned, &c.Completed, &c.LastAccessedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PinByVideoID pins the cache entry of a video's document (favoriting). It
// returns the doc id even when there is no entry yet, so the caller can queue
// the download.
func (d *DB) PinByVideoID(ctx context.Context, videoID int64) (int64, bool, error) {
	row := d.QueryRow(ctx, `
        UPDATE cache_entries SET pinned=true
        WHERE kind='video' AND tg_doc_id=(SELECT tg_doc_id FROM videos WHERE id=$1)
        RETURNING tg_doc_id, completed
    `, videoID)
	var docID int64
	var completed bool
	if err := row.Scan(&docID, &completed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			row2 := d.QueryRow(ctx, `SELECT tg_doc_id FROM videos WHERE id=$1`, videoID)
			if err := row2.Scan(&docID); err != nil {
				return 0, false, err
			}
			return docID, false, nil
		}
		return 0, false, err
	}
	return docID, completed, nil
}

func (d *DB) UnpinIfNotFavorited(ctx context.Context, videoID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE cache_entries SET pinned=false
        WHERE kind='video' AND tg_doc_id=(SELECT tg_doc_id FROM videos WHERE id=$1)
          AND NOT EXISTS (SELECT 1 FROM favorites f JOIN videos v ON v.id=f.video_id WHERE v.tg_doc_id=cache_entries.tg_doc_id)
    `, videoID)
	return err
}

// PinByPhotoID / UnpinPhotoIfNotFavorited mirror the video versions for images.
// Same dedup rule: one photo id on disk no matter how many rows reference it,
// so the unpin only fires once nobody has it favorited.
func (d *DB) PinByPhotoID(ctx context.Context, photoID int64) (int64, bool, error) {
	row := d.QueryRow(ctx, `
        UPDATE cache_entries SET pinned=true
        WHERE kind='photo' AND tg_doc_id=(SELECT tg_photo_id FROM photos WHERE id=$1)
        RETURNING tg_doc_id, completed
    `, photoID)
	var id int64
	var completed bool
	if err := row.Scan(&id, &completed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			row2 := d.QueryRow(ctx, `SELECT COALESCE(tg_photo_id, 0) FROM photos WHERE id=$1`, photoID)
			if err := row2.Scan(&id); err != nil {
				return 0, false, err
			}
			return id, false, nil
		}
		return 0, false, err
	}
	return id, completed, nil
}

func (d *DB) UnpinPhotoIfNotFavorited(ctx context.Context, photoID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE cache_entries SET pinned=false
        WHERE kind='photo' AND tg_doc_id=(SELECT tg_photo_id FROM photos WHERE id=$1)
          AND NOT EXISTS (
              SELECT 1 FROM photo_favorites f JOIN photos p ON p.id=f.photo_id
              WHERE p.tg_photo_id=cache_entries.tg_doc_id
          )
    `, photoID)
	return err
}

func (d *DB) DeleteCacheEntry(ctx context.Context, kind string, docID int64) error {
	_, err := d.Exec(ctx, `DELETE FROM cache_entries WHERE kind=$1 AND tg_doc_id=$2`, kind, docID)
	return err
}

func (d *DB) LookupDocByVideoID(ctx context.Context, videoID int64) (int64, error) {
	var doc int64
	err := d.QueryRow(ctx, `SELECT tg_doc_id FROM videos WHERE id=$1`, videoID).Scan(&doc)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return doc, err
}
