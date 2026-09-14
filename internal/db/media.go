package db

import (
	"context"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

// Media kinds. A "media item" is the union of a videos row and a photos row —
// the two live in separate tables (different Telegram locator shapes) but a
// channel/topic view has to show them interleaved by date.
const (
	MediaKindVideo = "video"
	MediaKindPhoto = "photo"
)

// MediaItem is one row of the merged list. Exactly one of Video/Photo is set.
type MediaItem struct {
	Kind  string
	Video *Video
	Photo *Photo
}

func (m MediaItem) id() int64 {
	if m.Video != nil {
		return m.Video.ID
	}
	return m.Photo.ID
}

// ChannelID is the channel (or topic) row the item belongs to.
func (m MediaItem) ChannelID() int64 {
	if m.Video != nil {
		return m.Video.ChannelID
	}
	return m.Photo.ChannelID
}

func (m MediaItem) date() *time.Time {
	if m.Video != nil {
		return m.Video.Date
	}
	return m.Photo.Date
}

func (m MediaItem) fileName() string {
	if m.Video != nil {
		return m.Video.FileName
	}
	return m.Photo.FileName
}

// MediaCursor is the keyset position of a merged list: one cursor per table,
// because each branch is paginated with its own (sort col, id) keyset.
//
// A single merged cursor would be wrong here: videos.id and photos.id are
// independent BIGSERIAL sequences, so "id < $n" means nothing across tables.
// Two cursors also keep each branch on its own index instead of forcing a
// UNION + sort of both whole tables.
type MediaCursor struct {
	VideoID int64 `json:"video,omitempty"`
	PhotoID int64 `json:"photo,omitempty"`
}

// ListMediaOpts is the union of SearchVideosOpts and SearchPhotosOpts plus the
// kind filter. Every field is optional; the zero value lists everything the
// user owns, newest first.
type ListMediaOpts struct {
	UserID    int64
	ChannelID int64
	Kind      string // "" = both, MediaKindVideo, MediaKindPhoto
	Q         string
	Text      string
	FileName  string
	DateFrom  *time.Time
	DateTo    *time.Time
	FavOnly   bool
	OrderBy   string
	Limit     int
	Cursor    MediaCursor

	// StreamerFilter scopes to one streamer (a videos-only filename convention),
	// which implies Kind == video.
	StreamerFilter bool
	Streamer       string
}

// ListMedia returns one page of the merged video+photo list.
//
// How the paging works: each table is queried for its own top `Limit` rows
// after its own cursor, the two pages are merged, and the result is truncated
// back to `Limit`. The returned cursor points at the last row of each kind that
// actually made it into this page, so the next call re-fetches whatever was
// read but not returned. That is correct by construction — the global top-N is
// always a subset of (top-N of videos ∪ top-N of photos) — and it never needs a
// cross-table comparison of ids.
func (d *DB) ListMedia(ctx context.Context, opt ListMediaOpts) ([]MediaItem, MediaCursor, bool, error) {
	limit := opt.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	// "duration" ordering has no meaning for photos — restrict to videos rather
	// than inventing a sort key that the two branches wouldn't agree on.
	if opt.OrderBy == "duration" {
		opt.Kind = MediaKindVideo
	}
	// The streamer bucket is derived from videos.file_name — images have no
	// equivalent, so filtering by one means "videos only".
	if opt.StreamerFilter {
		opt.Kind = MediaKindVideo
	}

	next := opt.Cursor
	items := make([]MediaItem, 0, limit*2)
	fullBranch := false

	if opt.Kind != MediaKindPhoto {
		vids, err := d.SearchVideos(ctx, SearchVideosOpts{
			UserID:    opt.UserID,
			Q:         opt.Q,
			Text:      opt.Text,
			FileName:  opt.FileName,
			DateFrom:  opt.DateFrom,
			DateTo:    opt.DateTo,
			ChannelID: opt.ChannelID,
			FavOnly:   opt.FavOnly,
			OrderBy:   opt.OrderBy,
			Limit:     limit,
			OffsetID:  opt.Cursor.VideoID,

			StreamerFilter: opt.StreamerFilter,
			Streamer:       opt.Streamer,
		})
		if err != nil {
			return nil, next, false, err
		}
		if len(vids) == limit {
			fullBranch = true
		}
		for i := range vids {
			items = append(items, MediaItem{Kind: MediaKindVideo, Video: &vids[i]})
		}
	}
	if opt.Kind != MediaKindVideo {
		phs, err := d.SearchPhotos(ctx, SearchPhotosOpts{
			UserID:    opt.UserID,
			Q:         opt.Q,
			Text:      opt.Text,
			FileName:  opt.FileName,
			DateFrom:  opt.DateFrom,
			DateTo:    opt.DateTo,
			ChannelID: opt.ChannelID,
			FavOnly:   opt.FavOnly,
			OrderBy:   opt.OrderBy,
			Limit:     limit,
			OffsetID:  opt.Cursor.PhotoID,
		})
		if err != nil {
			return nil, next, false, err
		}
		if len(phs) == limit {
			fullBranch = true
		}
		for i := range phs {
			items = append(items, MediaItem{Kind: MediaKindPhoto, Photo: &phs[i]})
		}
	}

	sortMedia(items, opt.OrderBy)
	hasMore := fullBranch || len(items) > limit
	if len(items) > limit {
		items = items[:limit]
	}
	for _, it := range items {
		if it.Kind == MediaKindVideo {
			next.VideoID = it.id()
		} else {
			next.PhotoID = it.id()
		}
	}
	return items, next, hasMore, nil
}

// sortMedia orders the merged page exactly like the per-table ORDER BY clauses
// (orderClauseOn): sort column first with NULLs at the tail, then id in the
// same direction. Kind is the final tie-break so the order is deterministic
// when a video and a photo share both the sort value and the id — ids are per
// table, so that collision is entirely possible.
func sortMedia(items []MediaItem, orderBy string) {
	col, asc := orderColumn(orderBy)
	if col == "" {
		col, asc = "date", false
	}
	less := func(a, b MediaItem) bool {
		var cmp int
		if col == "file_name" {
			cmp = compareNullableString(a.fileName(), b.fileName(), asc)
		} else {
			cmp = compareNullableTime(a.date(), b.date(), asc)
		}
		if cmp != 0 {
			return cmp < 0
		}
		if a.id() != b.id() {
			if asc {
				return a.id() < b.id()
			}
			return a.id() > b.id()
		}
		return a.Kind < b.Kind
	}
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
}

// compareNullableTime returns <0 when a sorts before b. Empty (NULL) values
// always go last, matching "NULLS LAST" in both directions.
func compareNullableTime(a, b *time.Time, asc bool) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case a.Equal(*b):
		return 0
	case a.Before(*b):
		if asc {
			return -1
		}
		return 1
	default:
		if asc {
			return 1
		}
		return -1
	}
}

// compareNullableString mirrors compareNullableTime for text columns; "" is the
// Go-side stand-in for SQL NULL (the column is COALESCE'd on read).
func compareNullableString(a, b string, asc bool) int {
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	case a == b:
		return 0
	case a < b:
		if asc {
			return -1
		}
		return 1
	default:
		if asc {
			return 1
		}
		return -1
	}
}

// MaxMsgIDForChannel / MinMsgIDForChannel are the sync cursors. They must span
// BOTH tables: a channel now stores videos and photos from the same message
// stream, so a videos-only MAX would make an incremental sync re-walk every
// message newer than the last *video* (and a videos-only MIN would make the
// backfill skip the gap it already covered).
func (d *DB) MaxMsgIDForChannel(ctx context.Context, channelID, userID int64) (int64, error) {
	row := d.QueryRow(ctx, `
        SELECT GREATEST(
            (SELECT COALESCE(MAX(tg_msg_id), 0) FROM videos WHERE channel_id=$1 AND user_id=$2),
            (SELECT COALESCE(MAX(tg_msg_id), 0) FROM photos WHERE channel_id=$1 AND user_id=$2)
        )
    `, channelID, userID)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (d *DB) MinMsgIDForChannel(ctx context.Context, channelID, userID int64) (int64, error) {
	// NULLIF(...,0) so an empty table doesn't drag the minimum down to 0.
	row := d.QueryRow(ctx, `
        SELECT COALESCE(LEAST(
            NULLIF((SELECT COALESCE(MIN(tg_msg_id), 0) FROM videos WHERE channel_id=$1 AND user_id=$2), 0),
            NULLIF((SELECT COALESCE(MIN(tg_msg_id), 0) FROM photos WHERE channel_id=$1 AND user_id=$2), 0)
        ), 0)
    `, channelID, userID)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// execBatch runs a queued batch and drains exactly n results, returning the
// first error. Every statement in our batches ends in RETURNING id, so results
// are consumed with QueryRow rather than Exec.
//
// The BatchResults must be fully drained and closed before the pooled
// connection is usable again, hence the unconditional Close.
func execBatch(ctx context.Context, d *DB, b *pgx.Batch, n int) error {
	br := d.SendBatch(ctx, b)
	var firstErr error
	for i := 0; i < n; i++ {
		var id int64
		if err := br.QueryRow().Scan(&id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := br.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// execBatchNoResult is execBatch for statements that return no rows.
func execBatchNoResult(ctx context.Context, d *DB, b *pgx.Batch, n int) error {
	br := d.SendBatch(ctx, b)
	var firstErr error
	for i := 0; i < n; i++ {
		if _, err := br.Exec(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := br.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// PropagateCaptions spreads each album's caption onto its silent siblings for a
// whole page at once — one round trip instead of one per album member.
//
// Sync calls this once per page with the distinct grouped_ids it just wrote,
// rather than once per message: a channel of image albums otherwise doubles its
// write cost for no extra effect (the UPDATE is idempotent, and running it once
// after all of a page's members are in place is strictly more likely to find
// the captioned member than running it after each one).
func (d *DB) PropagateCaptions(ctx context.Context, userID, channelID int64, videoGroups, photoGroups []int64) error {
	b := &pgx.Batch{}
	n := 0
	for _, g := range videoGroups {
		if g == 0 {
			continue
		}
		b.Queue(propagateVideoCaptionSQL, userID, channelID, g)
		n++
	}
	for _, g := range photoGroups {
		if g == 0 {
			continue
		}
		b.Queue(propagatePhotoCaptionSQL, userID, channelID, g)
		n++
	}
	if n == 0 {
		return nil
	}
	return execBatchNoResult(ctx, d, b, n)
}

// ClearThumbPaths forgets the on-disk thumbnails of the given rows, in one
// round trip. Called by the cache GC after it reclaims thumbnail files so the
// DB doesn't keep claiming files that are gone.
//
// Nothing actually *reads* thumb_path — the serving path derives the filename
// from the row id — so a stale value is only a bookkeeping lie, not a broken
// image. Keeping it honest is still worth one batched UPDATE.
func (d *DB) ClearThumbPaths(ctx context.Context, videoIDs, photoIDs []int64) error {
	b := &pgx.Batch{}
	n := 0
	for _, id := range videoIDs {
		b.Queue(`UPDATE videos SET thumb_path=NULL WHERE id=$1`, id)
		n++
	}
	for _, id := range photoIDs {
		b.Queue(`UPDATE photos SET thumb_path=NULL WHERE id=$1`, id)
		n++
	}
	if n == 0 {
		return nil
	}
	return execBatchNoResult(ctx, d, b, n)
}
