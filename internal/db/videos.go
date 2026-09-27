package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Video mirrors a TG Desktop JSON message (see migration 0003 for column docs)
// plus three lazily-resolved fields needed for streaming (TGDocID / AccessHash
// / FileReference).
type Video struct {
	ID        int64
	UserID    int64
	ChannelID int64

	// Mirror of JSON fields (snake_case → CamelCase by Go convention)
	TGMsgID           int64
	MsgType           string
	Date              *time.Time
	Edited            *time.Time
	FromName          string
	FromID            string
	File              string
	FileName          string
	FileSize          int64
	Thumbnail         string
	ThumbnailFileSize int64
	MediaType         string
	MimeType          string
	DurationSeconds   int
	Width             int
	Height            int
	Text              string
	TextEntities      []byte // raw JSON; nil = none

	// GroupedID is Telegram's album id (0 = not part of an album). Members of
	// the same album share it; the caption lives on one member and is
	// propagated to the rest so all are searchable.
	GroupedID int64

	// Streaming locator (lazy-filled on first play)
	TGDocID       int64
	AccessHash    int64
	FileReference []byte

	// DCID is the data center the document lives on (0 = unknown). Used to route
	// file transfers through the right per-DC connection pool.
	DCID int

	// ThumbSize is the PhotoSize.Type of the document thumbnail picked at sync
	// time (empty = the document has no usable thumb, or the row predates
	// thumbnails). ThumbPath is the already-downloaded thumb, relative to
	// CACHE_DIR (empty = not fetched yet).
	ThumbSize string
	ThumbPath string

	// FavoritedAt is when the row was added to the listing's membership — the
	// user's favorites, or a collection. Not a videos column: it is filled only
	// by those listings, from the join.
	FavoritedAt *time.Time
}

// All columns prefixed with v. so SELECT works even when the FROM clause
// joins another table with overlapping column names (favorites has user_id,
// causing "column reference is ambiguous (SQLSTATE 42702)" without the
// alias).
const videoCols = `
    v.id, v.user_id, v.channel_id,
    v.tg_msg_id, COALESCE(v.msg_type, ''),
    v.date, v.edited,
    COALESCE(v.from_name, ''), COALESCE(v.from_id, ''),
    COALESCE(v.file, ''), COALESCE(v.file_name, ''), COALESCE(v.file_size, 0),
    COALESCE(v.thumbnail, ''), COALESCE(v.thumbnail_file_size, 0),
    COALESCE(v.media_type, ''), COALESCE(v.mime_type, ''),
    COALESCE(v.duration_seconds, 0), COALESCE(v.width, 0), COALESCE(v.height, 0),
    COALESCE(v.text, ''), v.text_entities,
    COALESCE(v.tg_doc_id, 0), COALESCE(v.access_hash, 0), v.file_reference,
    COALESCE(v.grouped_id, 0), COALESCE(v.dc_id, 0),
    COALESCE(v.thumb_size, ''), COALESCE(v.thumb_path, '')
`

// scanVideo reads videoCols; extra lets a query that selects columns after
// them (the favorites join's created_at) scan those too.
func scanVideo(row pgx.Row, extra ...any) (*Video, error) {
	v := &Video{}
	dest := []any{
		&v.ID, &v.UserID, &v.ChannelID,
		&v.TGMsgID, &v.MsgType,
		&v.Date, &v.Edited,
		&v.FromName, &v.FromID,
		&v.File, &v.FileName, &v.FileSize,
		&v.Thumbnail, &v.ThumbnailFileSize,
		&v.MediaType, &v.MimeType,
		&v.DurationSeconds, &v.Width, &v.Height,
		&v.Text, &v.TextEntities,
		&v.TGDocID, &v.AccessHash, &v.FileReference,
		&v.GroupedID, &v.DCID,
		&v.ThumbSize, &v.ThumbPath,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	return v, nil
}

// upsertVideoSQL is shared by the single-row and batched writers so the two can
// never drift apart.
const upsertVideoSQL = `
        INSERT INTO videos (
            user_id, channel_id,
            tg_msg_id, msg_type, date, edited,
            from_name, from_id,
            file, file_name, file_size,
            thumbnail, thumbnail_file_size,
            media_type, mime_type,
            duration_seconds, width, height,
            text, text_entities, grouped_id, dc_id, thumb_size,
            tg_doc_id, access_hash, file_reference
        ) VALUES (
            $1,$2,
            $3,$4,$5,$6,
            $7,$8,
            $9,$10,$11,
            $12,$13,
            $14,$15,
            $16,$17,$18,
            $19,$20,$21,$22,$23,
            $24,$25,$26
        )
        ON CONFLICT (user_id, channel_id, tg_msg_id) DO UPDATE SET
            msg_type            = EXCLUDED.msg_type,
            date                = EXCLUDED.date,
            edited              = EXCLUDED.edited,
            from_name           = EXCLUDED.from_name,
            from_id             = EXCLUDED.from_id,
            file                = EXCLUDED.file,
            file_name           = EXCLUDED.file_name,
            file_size           = EXCLUDED.file_size,
            thumbnail           = EXCLUDED.thumbnail,
            thumbnail_file_size = EXCLUDED.thumbnail_file_size,
            media_type          = EXCLUDED.media_type,
            mime_type           = EXCLUDED.mime_type,
            duration_seconds    = EXCLUDED.duration_seconds,
            width               = EXCLUDED.width,
            height              = EXCLUDED.height,
            -- Don't let a re-synced silent album sibling (empty text) clobber a
            -- caption we propagated to it; keep the existing text in that case.
            text                = COALESCE(NULLIF(EXCLUDED.text, ''), videos.text),
            text_entities       = EXCLUDED.text_entities,
            grouped_id          = EXCLUDED.grouped_id,
            -- Keep a known DC if a later re-import (e.g. JSON) carries 0.
            dc_id               = CASE WHEN EXCLUDED.dc_id > 0 THEN EXCLUDED.dc_id ELSE videos.dc_id END,
            -- Same for the thumb size: a JSON re-import has none, don't wipe it.
            thumb_size          = COALESCE(EXCLUDED.thumb_size, videos.thumb_size),
            -- TG sync carries the streaming locator; the JSON importer does not
            -- (it inserts 0/empty). Only overwrite when the incoming row actually
            -- has one, so a re-import can't wipe what sync resolved.
            tg_doc_id           = CASE WHEN EXCLUDED.tg_doc_id > 0 THEN EXCLUDED.tg_doc_id ELSE videos.tg_doc_id END,
            access_hash         = CASE WHEN EXCLUDED.tg_doc_id > 0 THEN EXCLUDED.access_hash ELSE videos.access_hash END,
            file_reference      = CASE WHEN EXCLUDED.tg_doc_id > 0 THEN EXCLUDED.file_reference ELSE videos.file_reference END
        RETURNING id
    `

func upsertVideoArgs(v *Video) []any {
	return []any{
		v.UserID, v.ChannelID,
		v.TGMsgID, nilIfEmpty(v.MsgType), v.Date, v.Edited,
		nilIfEmpty(v.FromName), nilIfEmpty(v.FromID),
		nilIfEmpty(v.File), nilIfEmpty(v.FileName), v.FileSize,
		nilIfEmpty(v.Thumbnail), v.ThumbnailFileSize,
		nilIfEmpty(v.MediaType), nilIfEmpty(v.MimeType),
		v.DurationSeconds, v.Width, v.Height,
		nilIfEmpty(v.Text), v.TextEntities, nilIfZero64(v.GroupedID), v.DCID,
		nilIfEmpty(v.ThumbSize),
		v.TGDocID, v.AccessHash, v.FileReference,
	}
}

// UpsertVideo writes a row from JSON import (idempotent on tg_msg_id).
func (d *DB) UpsertVideo(ctx context.Context, v *Video) (int64, error) {
	row := d.QueryRow(ctx, upsertVideoSQL, upsertVideoArgs(v)...)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// UpsertVideos writes a whole page of rows in ONE network round trip.
//
// This is the hot path of a full sync: a big channel is ~1M messages, and one
// round trip per row (plus one per album caption propagation) means millions of
// them — that, not Telegram, is what makes a first sync take a day. pgx runs a
// batch in an implicit transaction, so a page is all-or-nothing, which is also
// what resumable sync wants: no half-written page to reason about.
func (d *DB) UpsertVideos(ctx context.Context, vs []*Video) error {
	if len(vs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, v := range vs {
		b.Queue(upsertVideoSQL, upsertVideoArgs(v)...)
	}
	return execBatch(ctx, d, b, len(vs))
}

// nilIfZero64 maps 0 → SQL NULL so non-album rows leave grouped_id NULL (and
// out of the partial index) instead of clustering under a bogus 0 group.
func nilIfZero64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// propagateVideoCaptionSQL is shared with the batched page-level propagation in
// media.go.
const propagateVideoCaptionSQL = `
        WITH cap AS (
            SELECT text, text_entities
            FROM videos
            WHERE user_id=$1 AND channel_id=$2 AND grouped_id=$3
              AND COALESCE(text, '') <> ''
            LIMIT 1
        )
        UPDATE videos v
        SET text = cap.text, text_entities = cap.text_entities
        FROM cap
        WHERE v.user_id=$1 AND v.channel_id=$2 AND v.grouped_id=$3
          AND COALESCE(v.text, '') = ''
    `

// PropagateGroupCaption copies the caption of an album (one member carries it)
// onto every sibling in the same group whose text is still empty, so a text
// search matches all of the album's videos, not just the captioned one.
// Idempotent and order-independent: safe to call after writing any member.
func (d *DB) PropagateGroupCaption(ctx context.Context, userID, channelID, groupedID int64) error {
	if groupedID == 0 {
		return nil
	}
	_, err := d.Exec(ctx, propagateVideoCaptionSQL, userID, channelID, groupedID)
	return err
}

// UpdateVideoLocator persists the TG streaming locator after first refresh.
// FileSize is overwritten only when newSize > 0; same for mime_type, dc_id and
// thumb_size.
func (d *DB) UpdateVideoLocator(ctx context.Context, id int64, tgDocID, accessHash int64, fr []byte, newSize int64, newMime string, dcID int, thumbSize string) error {
	_, err := d.Exec(ctx, `
        UPDATE videos SET
            tg_doc_id      = $2,
            access_hash    = $3,
            file_reference = $4,
            file_size      = CASE WHEN $5 > 0 THEN $5 ELSE file_size END,
            mime_type      = COALESCE(NULLIF($6, ''), mime_type),
            dc_id          = CASE WHEN $7 > 0 THEN $7 ELSE dc_id END,
            thumb_size     = COALESCE(NULLIF($8, ''), thumb_size)
        WHERE id=$1
    `, id, tgDocID, accessHash, fr, newSize, newMime, dcID, thumbSize)
	return err
}

// SetVideoThumbPath records the on-disk thumbnail (relative to CACHE_DIR) after
// it has been fetched from Telegram. Empty path clears it (thumb file lost).
func (d *DB) SetVideoThumbPath(ctx context.Context, id int64, path string) error {
	_, err := d.Exec(ctx, `UPDATE videos SET thumb_path=$2 WHERE id=$1`, id, nilIfEmpty(path))
	return err
}

func (d *DB) CountVideosByChannel(ctx context.Context, userID, channelID int64) (int64, error) {
	row := d.QueryRow(ctx, `SELECT count(*) FROM videos WHERE user_id=$1 AND channel_id=$2`, userID, channelID)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// The per-channel sync cursors used to live here as MaxTGMsgID/MinTGMsgID over
// the videos table alone. They now span videos+photos and live in media.go
// (MaxMsgIDForChannel / MinMsgIDForChannel) — a videos-only cursor would make
// an incremental sync re-walk everything newer than the last *video*.

func (d *DB) DeleteVideosByChannel(ctx context.Context, userID, channelID int64) (int64, error) {
	tag, err := d.Exec(ctx, `DELETE FROM videos WHERE user_id=$1 AND channel_id=$2`, userID, channelID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

type ListVideosOpts struct {
	UserID    int64
	ChannelID int64 // 0 = all
	Limit     int
	OffsetID  int64 // keyset cursor (smallest id from previous page)
	OrderBy   string
	FavOnly   bool

	// StreamerFilter enables filtering by Streamer. When true, Streamer=="" means
	// "the NULL bucket" (filenames not matching the streamer pattern). When
	// false, Streamer is ignored.
	StreamerFilter bool
	Streamer       string
}

// orderColumn maps an order key to the sort column and direction. An empty
// column means "no composite keyset" (duration / unknown) — fall back to a
// plain id cursor. Any unrecognized key is treated as the default date DESC.
func orderColumn(orderBy string) (col string, asc bool) {
	switch orderBy {
	case "date_asc":
		return "date", true
	case "name_asc":
		return "file_name", true
	case "name_desc":
		return "file_name", false
	case "duration":
		return "", false
	default: // "" / "date_desc"
		return "date", false
	}
}

// orderClause returns the ORDER BY for a given order key. id is always the
// tie-breaker in the same direction so the (col, id) tuple is a total order,
// which the keyset cursor relies on. NULLS LAST keeps captionless/undated rows
// at the tail in both directions.
func orderClause(orderBy string) string { return orderClauseOn("v", orderBy, true) }

// orderClauseOn is orderClause for an arbitrary alias/table. hasDuration=false
// (photos) maps the duration key onto the default date ordering, since the
// table has no duration column.
// Favorite-time ordering. Only meaningful for a favorites listing, which joins
// the favorites table as alias f; the list code falls back to the default order
// for anything else (normalizeFavOrder).
const (
	OrderFavDesc = "fav_desc"
	OrderFavAsc  = "fav_asc"
)

func isFavOrder(orderBy string) bool { return orderBy == OrderFavDesc || orderBy == OrderFavAsc }

// normalizeFavOrder picks the effective order for a list. A favorites listing
// with no explicit order sorts by when things were favorited, newest first —
// that's what "my favorites" means; a non-favorites listing can't use a
// favorite order at all (there is no f join), so it gets the default.
func normalizeFavOrder(orderBy string, favOnly bool) string {
	if favOnly && orderBy == "" {
		return OrderFavDesc
	}
	if !favOnly && isFavOrder(orderBy) {
		return ""
	}
	return orderBy
}

// membership is the table a listing is restricted to — the user's favorites,
// or one of their collections — joined as alias f. Its created_at is when the
// row was added, which is what the fav_* orders sort on, so a collection pages
// and sorts exactly like favorites.
type membership struct {
	table    string // favorites / photo_favorites / collection_videos / collection_photos
	fk       string // video_id / photo_id
	scopeCol string // user_id / collection_id
	scopeArg string // placeholder holding the scope value, e.g. "$1"
}

// membershipFor picks the join for a listing: a collection if one is given,
// else the user's favorites if favOnly, else none. A collection's id is added
// to args; favorites scope by the listing user, always $1.
func membershipFor(favOnly bool, collectionID int64, favTable, collTable, fk string, args *[]any) *membership {
	switch {
	case collectionID != 0:
		*args = append(*args, collectionID)
		return &membership{collTable, fk, "collection_id", "$" + itoa(len(*args))}
	case favOnly:
		return &membership{favTable, fk, "user_id", "$1"}
	}
	return nil
}

func (m *membership) join(alias string) string {
	return "JOIN " + m.table + " f ON f." + m.fk + "=" + alias + ".id AND f." + m.scopeCol + "=" + m.scopeArg + " "
}

// favKeyset is the cursor for added-time ordering. created_at is NOT NULL, so
// unlike keysetCursorOn there is no NULL tail to handle and a row comparison is
// exact. The boundary's added time is looked up by id within the same scope.
func favKeyset(m *membership, alias, orderBy, p string) string {
	cmp := "<"
	if orderBy == OrderFavAsc {
		cmp = ">"
	}
	return "(f.created_at, " + alias + ".id) " + cmp +
		" ((SELECT created_at FROM " + m.table + " WHERE " + m.scopeCol + " = " + m.scopeArg +
		" AND " + m.fk + " = $" + p + "), $" + p + ")"
}

func favOrderClause(alias, orderBy string) string {
	dir := "DESC"
	if orderBy == OrderFavAsc {
		dir = "ASC"
	}
	return " ORDER BY f.created_at " + dir + ", " + alias + ".id " + dir
}

func orderClauseOn(alias, orderBy string, hasDuration bool) string {
	col, asc := orderColumn(orderBy)
	if col == "" {
		if !hasDuration {
			return " ORDER BY " + alias + ".date DESC NULLS LAST, " + alias + ".id DESC"
		}
		// duration (or unknown non-date/name) — keep the legacy clause.
		return " ORDER BY " + alias + ".duration_seconds DESC, " + alias + ".id DESC"
	}
	dir := "DESC"
	if asc {
		dir = "ASC"
	}
	return " ORDER BY " + alias + "." + col + " " + dir + " NULLS LAST, " + alias + ".id " + dir
}

// keysetCursor builds the WHERE condition for "rows after the boundary row
// whose id = $p", matching the list's ORDER BY. The boundary row's sort key is
// looked up by id server-side, so callers only need to pass offset_id (no extra
// cursor params on the API/frontend).
//
// For the default date ordering (ORDER BY date DESC NULLS LAST, id DESC) a
// plain `id < $p` cursor is WRONG: id is BIGSERIAL (insert order) while sync
// writes incremental-at-top / backfill-at-bottom, so id order ≠ date order, and
// the mismatch makes a page come up short and pagination stop early. The
// composite (col, id) keyset below fixes that for date and file_name ordering;
// duration keeps the simple id cursor (not frontend-paginated).
func keysetCursor(orderBy, p string) string {
	return keysetCursorOn("videos", "v", orderBy, p, true)
}

// keysetCursorOn is keysetCursor for an arbitrary table/alias. hasDuration
// mirrors orderClauseOn: a table without a duration column (photos) falls back
// to the date keyset instead of the legacy plain-id cursor.
func keysetCursorOn(table, alias, orderBy, p string, hasDuration bool) string {
	col, asc := orderColumn(orderBy)
	if col == "" {
		if hasDuration {
			return alias + ".id < $" + p
		}
		col, asc = "date", false
	}
	cmp := "<"
	if asc {
		cmp = ">"
	}
	c := alias + "." + col
	id := alias + ".id"
	cur := "(SELECT " + col + " FROM " + table + " WHERE id = $" + p + ")"
	// Boundary in the NULL tail ⇒ only later NULLs (by id, same direction).
	// Otherwise: strictly past the boundary value, the tie broken by id, plus
	// the whole NULL tail (which sorts after any non-NULL value).
	return "CASE WHEN " + cur + " IS NULL " +
		"THEN (" + c + " IS NULL AND " + id + " " + cmp + " $" + p + ") " +
		"ELSE (" + c + " " + cmp + " " + cur + " OR (" + c + " = " + cur + " AND " + id + " " + cmp + " $" + p + ") OR " + c + " IS NULL) END"
}

func (d *DB) ListVideos(ctx context.Context, opt ListVideosOpts) ([]Video, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 200
	}
	q := `SELECT ` + videoCols + ` FROM videos v `
	args := []any{opt.UserID}
	where := []string{"v.user_id=$1"}
	if opt.ChannelID != 0 {
		args = append(args, opt.ChannelID)
		where = append(where, "v.channel_id=$"+itoa(len(args)))
	}
	if opt.OffsetID > 0 {
		args = append(args, opt.OffsetID)
		where = append(where, keysetCursor(opt.OrderBy, itoa(len(args))))
	}
	if opt.StreamerFilter {
		if opt.Streamer == "" {
			where = append(where, "v.streamer IS NULL")
		} else {
			args = append(args, opt.Streamer)
			where = append(where, "v.streamer = $"+itoa(len(args)))
		}
	}
	if opt.FavOnly {
		q += ` JOIN favorites f ON f.video_id=v.id AND f.user_id=v.user_id `
	}
	q += ` WHERE ` + joinWhere(where)
	q += orderClause(opt.OrderBy)
	args = append(args, opt.Limit)
	q += ` LIMIT $` + itoa(len(args))
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		v, err := scanVideo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (d *DB) VideoByID(ctx context.Context, id, userID int64) (*Video, error) {
	row := d.QueryRow(ctx, `SELECT `+videoCols+` FROM videos v WHERE v.id=$1 AND v.user_id=$2`, id, userID)
	v, err := scanVideo(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return v, nil
}

// SearchVideosOpts: any subset of fields can be supplied. Empty are ignored.
// Q is a "match-either" shortcut that ORs `file_name` and `text`. Text /
// FileName are AND'd in addition (used by the advanced /search page).
type SearchVideosOpts struct {
	UserID    int64
	Q         string // OR-match on (file_name, text)
	Text      string // ILIKE on `text`
	FileName  string // ILIKE on `file_name`
	DateFrom  *time.Time
	DateTo    *time.Time
	ChannelID int64
	Limit     int
	OffsetID  int64
	OrderBy   string
	FavOnly   bool // restrict to the user's favorites (JOIN favorites)
	// CollectionID restricts to one collection (JOIN collection_videos); the
	// caller has checked it belongs to UserID. Takes precedence over FavOnly.
	CollectionID int64

	// StreamerFilter behaves exactly like ListVideosOpts': when true, Streamer
	// == "" means the NULL bucket (filenames that don't match the pattern).
	StreamerFilter bool
	Streamer       string
}

func (d *DB) SearchVideos(ctx context.Context, opt SearchVideosOpts) ([]Video, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 200
	}
	args := []any{opt.UserID}
	where := []string{"v.user_id=$1"}
	mem := membershipFor(opt.FavOnly, opt.CollectionID, "favorites", "collection_videos", "video_id", &args)
	opt.OrderBy = normalizeFavOrder(opt.OrderBy, mem != nil)
	if opt.Q != "" {
		args = append(args, "%"+opt.Q+"%")
		i := itoa(len(args))
		where = append(where, "(v.file_name ILIKE $"+i+" OR v.text ILIKE $"+i+")")
	}
	if opt.Text != "" {
		args = append(args, "%"+opt.Text+"%")
		where = append(where, "v.text ILIKE $"+itoa(len(args)))
	}
	if opt.FileName != "" {
		args = append(args, "%"+opt.FileName+"%")
		where = append(where, "v.file_name ILIKE $"+itoa(len(args)))
	}
	if opt.DateFrom != nil {
		args = append(args, *opt.DateFrom)
		where = append(where, "v.date >= $"+itoa(len(args)))
	}
	if opt.DateTo != nil {
		args = append(args, *opt.DateTo)
		where = append(where, "v.date <= $"+itoa(len(args)))
	}
	if opt.ChannelID != 0 {
		args = append(args, opt.ChannelID)
		where = append(where, "v.channel_id=$"+itoa(len(args)))
	}
	if opt.StreamerFilter {
		if opt.Streamer == "" {
			where = append(where, "v.streamer IS NULL")
		} else {
			args = append(args, opt.Streamer)
			where = append(where, "v.streamer = $"+itoa(len(args)))
		}
	}
	if opt.OffsetID > 0 {
		args = append(args, opt.OffsetID)
		if isFavOrder(opt.OrderBy) {
			where = append(where, favKeyset(mem, "v", opt.OrderBy, itoa(len(args))))
		} else {
			where = append(where, keysetCursor(opt.OrderBy, itoa(len(args))))
		}
	}
	args = append(args, opt.Limit)
	cols := videoCols
	from := `FROM videos v `
	order := orderClause(opt.OrderBy)
	if mem != nil {
		// Also return when each row was added: the favorites page groups by
		// it, and the merged video+photo list sorts on it.
		cols += `, f.created_at`
		from += mem.join("v")
		if isFavOrder(opt.OrderBy) {
			order = favOrderClause("v", opt.OrderBy)
		}
	}
	q := `SELECT ` + cols + ` ` + from + `WHERE ` + joinWhere(where) +
		order + ` LIMIT $` + itoa(len(args))
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		var favAt time.Time
		var extra []any
		if mem != nil {
			extra = []any{&favAt}
		}
		v, err := scanVideo(rows, extra...)
		if err != nil {
			return nil, err
		}
		if mem != nil {
			v.FavoritedAt = &favAt
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func joinWhere(parts []string) string {
	return strings.Join(parts, " AND ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
