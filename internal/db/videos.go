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
    COALESCE(v.grouped_id, 0), COALESCE(v.dc_id, 0)
`

func scanVideo(row pgx.Row) (*Video, error) {
	v := &Video{}
	if err := row.Scan(
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
	); err != nil {
		return nil, err
	}
	return v, nil
}

// UpsertVideo writes a row from JSON import (idempotent on tg_msg_id).
func (d *DB) UpsertVideo(ctx context.Context, v *Video) (int64, error) {
	row := d.QueryRow(ctx, `
        INSERT INTO videos (
            user_id, channel_id,
            tg_msg_id, msg_type, date, edited,
            from_name, from_id,
            file, file_name, file_size,
            thumbnail, thumbnail_file_size,
            media_type, mime_type,
            duration_seconds, width, height,
            text, text_entities, grouped_id, dc_id
        ) VALUES (
            $1,$2,
            $3,$4,$5,$6,
            $7,$8,
            $9,$10,$11,
            $12,$13,
            $14,$15,
            $16,$17,$18,
            $19,$20,$21,$22
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
            dc_id               = CASE WHEN EXCLUDED.dc_id > 0 THEN EXCLUDED.dc_id ELSE videos.dc_id END
        RETURNING id
    `,
		v.UserID, v.ChannelID,
		v.TGMsgID, nilIfEmpty(v.MsgType), v.Date, v.Edited,
		nilIfEmpty(v.FromName), nilIfEmpty(v.FromID),
		nilIfEmpty(v.File), nilIfEmpty(v.FileName), v.FileSize,
		nilIfEmpty(v.Thumbnail), v.ThumbnailFileSize,
		nilIfEmpty(v.MediaType), nilIfEmpty(v.MimeType),
		v.DurationSeconds, v.Width, v.Height,
		nilIfEmpty(v.Text), v.TextEntities, nilIfZero64(v.GroupedID), v.DCID,
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// nilIfZero64 maps 0 → SQL NULL so non-album rows leave grouped_id NULL (and
// out of the partial index) instead of clustering under a bogus 0 group.
func nilIfZero64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// PropagateGroupCaption copies the caption of an album (one member carries it)
// onto every sibling in the same group whose text is still empty, so a text
// search matches all of the album's videos, not just the captioned one.
// Idempotent and order-independent: safe to call after writing any member.
func (d *DB) PropagateGroupCaption(ctx context.Context, userID, channelID, groupedID int64) error {
	if groupedID == 0 {
		return nil
	}
	_, err := d.Exec(ctx, `
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
    `, userID, channelID, groupedID)
	return err
}

// UpdateVideoLocator persists the TG streaming locator after first refresh.
// FileSize is overwritten only when newSize > 0; same for mime_type and dc_id.
func (d *DB) UpdateVideoLocator(ctx context.Context, id int64, tgDocID, accessHash int64, fr []byte, newSize int64, newMime string, dcID int) error {
	_, err := d.Exec(ctx, `
        UPDATE videos SET
            tg_doc_id      = $2,
            access_hash    = $3,
            file_reference = $4,
            file_size      = CASE WHEN $5 > 0 THEN $5 ELSE file_size END,
            mime_type      = COALESCE(NULLIF($6, ''), mime_type),
            dc_id          = CASE WHEN $7 > 0 THEN $7 ELSE dc_id END
        WHERE id=$1
    `, id, tgDocID, accessHash, fr, newSize, newMime, dcID)
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

// MaxTGMsgID returns the largest tg_msg_id stored for a channel — TG sync
// uses it to skip messages we've already imported and only fetch newer ones.
// Returns 0 when the channel is empty.
func (d *DB) MaxTGMsgID(ctx context.Context, channelID, userID int64) (int64, error) {
	row := d.QueryRow(ctx, `
        SELECT COALESCE(MAX(tg_msg_id), 0) FROM videos
        WHERE channel_id=$1 AND user_id=$2
    `, channelID, userID)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// MinTGMsgID returns the smallest tg_msg_id stored for a channel — resumable
// sync uses it as the backfill cursor (fetch messages older than this).
// Returns 0 when the channel is empty.
func (d *DB) MinTGMsgID(ctx context.Context, channelID, userID int64) (int64, error) {
	row := d.QueryRow(ctx, `
        SELECT COALESCE(MIN(tg_msg_id), 0) FROM videos
        WHERE channel_id=$1 AND user_id=$2
    `, channelID, userID)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

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
func orderClause(orderBy string) string {
	col, asc := orderColumn(orderBy)
	if col == "" {
		// duration (or unknown non-date/name) — keep the legacy clause.
		return " ORDER BY v.duration_seconds DESC, v.id DESC"
	}
	dir := "DESC"
	if asc {
		dir = "ASC"
	}
	return " ORDER BY v." + col + " " + dir + " NULLS LAST, v.id " + dir
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
	col, asc := orderColumn(orderBy)
	if col == "" {
		return "v.id < $" + p
	}
	cmp := "<"
	if asc {
		cmp = ">"
	}
	c := "v." + col
	cur := "(SELECT " + col + " FROM videos WHERE id = $" + p + ")"
	// Boundary in the NULL tail ⇒ only later NULLs (by id, same direction).
	// Otherwise: strictly past the boundary value, the tie broken by id, plus
	// the whole NULL tail (which sorts after any non-NULL value).
	return "CASE WHEN " + cur + " IS NULL " +
		"THEN (" + c + " IS NULL AND v.id " + cmp + " $" + p + ") " +
		"ELSE (" + c + " " + cmp + " " + cur + " OR (" + c + " = " + cur + " AND v.id " + cmp + " $" + p + ") OR " + c + " IS NULL) END"
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
}

func (d *DB) SearchVideos(ctx context.Context, opt SearchVideosOpts) ([]Video, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 200
	}
	args := []any{opt.UserID}
	where := []string{"v.user_id=$1"}
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
	if opt.OffsetID > 0 {
		args = append(args, opt.OffsetID)
		where = append(where, keysetCursor(opt.OrderBy, itoa(len(args))))
	}
	args = append(args, opt.Limit)
	from := `FROM videos v `
	if opt.FavOnly {
		from += `JOIN favorites f ON f.video_id=v.id AND f.user_id=v.user_id `
	}
	q := `SELECT ` + videoCols + ` ` + from + `WHERE ` + joinWhere(where) +
		orderClause(opt.OrderBy) + ` LIMIT $` + itoa(len(args))
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
