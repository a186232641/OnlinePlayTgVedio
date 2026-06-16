package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type CacheEntry struct {
	TGDocID   int64
	FilePath  string
	Bytes     int64
	Pinned    bool
	Completed bool
}

func (d *DB) GetCacheEntry(ctx context.Context, docID int64) (*CacheEntry, error) {
	row := d.QueryRow(ctx, `
        SELECT tg_doc_id, file_path, bytes, pinned, completed
        FROM cache_entries WHERE tg_doc_id=$1
    `, docID)
	c := &CacheEntry{}
	if err := row.Scan(&c.TGDocID, &c.FilePath, &c.Bytes, &c.Pinned, &c.Completed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (d *DB) UpsertCacheEntry(ctx context.Context, c *CacheEntry) error {
	_, err := d.Exec(ctx, `
        INSERT INTO cache_entries (tg_doc_id, file_path, bytes, pinned, completed, last_accessed_at)
        VALUES ($1, $2, $3, $4, $5, NOW())
        ON CONFLICT (tg_doc_id) DO UPDATE SET
            file_path = EXCLUDED.file_path,
            bytes     = EXCLUDED.bytes,
            pinned    = cache_entries.pinned OR EXCLUDED.pinned,
            completed = cache_entries.completed OR EXCLUDED.completed,
            last_accessed_at = NOW()
    `, c.TGDocID, c.FilePath, c.Bytes, c.Pinned, c.Completed)
	return err
}

func (d *DB) MarkCacheCompleted(ctx context.Context, docID int64, bytes int64) error {
	_, err := d.Exec(ctx, `
        UPDATE cache_entries SET completed=true, bytes=$2, last_accessed_at=NOW() WHERE tg_doc_id=$1
    `, docID, bytes)
	return err
}

// MarkCacheIncomplete resets an entry to not-completed (e.g. when the on-disk
// file failed an integrity check), so the next play re-downloads it. The pinned
// flag is preserved, so favorites stay pinned across the re-download.
func (d *DB) MarkCacheIncomplete(ctx context.Context, docID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE cache_entries SET completed=false, bytes=0 WHERE tg_doc_id=$1
    `, docID)
	return err
}

func (d *DB) TouchCache(ctx context.Context, docID int64) error {
	_, err := d.Exec(ctx, `UPDATE cache_entries SET last_accessed_at=NOW() WHERE tg_doc_id=$1`, docID)
	return err
}

func (d *DB) SetCachePinned(ctx context.Context, docID int64, pinned bool) error {
	_, err := d.Exec(ctx, `UPDATE cache_entries SET pinned=$2 WHERE tg_doc_id=$1`, docID, pinned)
	return err
}

// PinFavoriteCacheByVideo marks the cache entry of the favorited video as
// pinned (creating a placeholder if none exists yet, so the cache worker can
// see it and start downloading).
func (d *DB) PinByVideoID(ctx context.Context, videoID int64) (int64, bool, error) {
	row := d.QueryRow(ctx, `
        UPDATE cache_entries
        SET pinned = true
        WHERE tg_doc_id = (SELECT tg_doc_id FROM videos WHERE id=$1)
        RETURNING tg_doc_id, completed
    `, videoID)
	var docID int64
	var completed bool
	if err := row.Scan(&docID, &completed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// no entry yet → look up doc id
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
        WHERE tg_doc_id = (SELECT tg_doc_id FROM videos WHERE id=$1)
          AND NOT EXISTS (
              SELECT 1 FROM favorites f
                JOIN videos v ON v.id=f.video_id
                WHERE v.tg_doc_id = cache_entries.tg_doc_id
          )
    `, videoID)
	return err
}

// LRUCandidates returns rows eligible for eviction in oldest-first order.
func (d *DB) LRUCandidates(ctx context.Context, max int) ([]CacheEntry, error) {
	rows, err := d.Query(ctx, `
        SELECT tg_doc_id, file_path, bytes, pinned, completed
        FROM cache_entries
        WHERE completed=true AND pinned=false
        ORDER BY last_accessed_at ASC
        LIMIT $1
    `, max)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CacheEntry
	for rows.Next() {
		var c CacheEntry
		if err := rows.Scan(&c.TGDocID, &c.FilePath, &c.Bytes, &c.Pinned, &c.Completed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) DeleteCacheEntry(ctx context.Context, docID int64) error {
	_, err := d.Exec(ctx, `DELETE FROM cache_entries WHERE tg_doc_id=$1`, docID)
	return err
}

func (d *DB) TotalCacheBytes(ctx context.Context) (int64, error) {
	var total int64
	err := d.QueryRow(ctx, `SELECT COALESCE(SUM(bytes),0) FROM cache_entries WHERE completed=true`).Scan(&total)
	return total, err
}

// LookupDocByVideoID returns the tg_doc_id for a given video.
func (d *DB) LookupDocByVideoID(ctx context.Context, videoID int64) (int64, error) {
	var doc int64
	err := d.QueryRow(ctx, `SELECT tg_doc_id FROM videos WHERE id=$1`, videoID).Scan(&doc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return doc, nil
}
