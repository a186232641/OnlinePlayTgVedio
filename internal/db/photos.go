package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Photo is one image message. It deliberately mirrors Video's shape (same JSON
// export-derived column names, same lazily-resolved locator idea) so the two
// can be merged into one media list — but the locator is a *photo* locator
// (tg.InputPhotoFileLocation: photo id + access_hash + file_reference + a size
// type), which is a different id space from documents. See migration 0010.
type Photo struct {
	ID        int64
	UserID    int64
	ChannelID int64

	TGMsgID      int64
	MsgType      string
	Date         *time.Time
	Edited       *time.Time
	FromName     string
	FromID       string
	File         string
	FileName     string
	FileSize     int64
	Width        int
	Height       int
	Text         string
	TextEntities []byte
	GroupedID    int64

	// Locator (lazy-filled on first view for JSON-imported rows)
	TGPhotoID     int64
	AccessHash    int64
	FileReference []byte
	DCID          int
	// SizeType is the PhotoSize.Type of the full-size image we serve ("y"/"x"…);
	// ThumbSize the smaller one used for grid thumbnails ("m"/"s"…). Both are
	// picked at sync time from Photo.Sizes.
	SizeType  string
	ThumbSize string
	// ThumbPath is the already-downloaded thumbnail, relative to CACHE_DIR.
	ThumbPath string
}

// Columns are aliased p.* so the SELECT survives a JOIN with photo_favorites
// (which also has user_id).
const photoCols = `
    p.id, p.user_id, p.channel_id,
    p.tg_msg_id, COALESCE(p.msg_type, ''),
    p.date, p.edited,
    COALESCE(p.from_name, ''), COALESCE(p.from_id, ''),
    COALESCE(p.file, ''), COALESCE(p.file_name, ''), COALESCE(p.file_size, 0),
    COALESCE(p.width, 0), COALESCE(p.height, 0),
    COALESCE(p.text, ''), p.text_entities, COALESCE(p.grouped_id, 0),
    COALESCE(p.tg_photo_id, 0), COALESCE(p.access_hash, 0), p.file_reference,
    COALESCE(p.dc_id, 0), COALESCE(p.size_type, ''), COALESCE(p.thumb_size, ''),
    COALESCE(p.thumb_path, '')
`

func scanPhoto(row pgx.Row) (*Photo, error) {
	p := &Photo{}
	if err := row.Scan(
		&p.ID, &p.UserID, &p.ChannelID,
		&p.TGMsgID, &p.MsgType,
		&p.Date, &p.Edited,
		&p.FromName, &p.FromID,
		&p.File, &p.FileName, &p.FileSize,
		&p.Width, &p.Height,
		&p.Text, &p.TextEntities, &p.GroupedID,
		&p.TGPhotoID, &p.AccessHash, &p.FileReference,
		&p.DCID, &p.SizeType, &p.ThumbSize,
		&p.ThumbPath,
	); err != nil {
		return nil, err
	}
	return p, nil
}

const upsertPhotoSQL = `
        INSERT INTO photos (
            user_id, channel_id,
            tg_msg_id, msg_type, date, edited,
            from_name, from_id,
            file, file_name, file_size,
            width, height,
            text, text_entities, grouped_id,
            tg_photo_id, access_hash, file_reference, dc_id,
            size_type, thumb_size
        ) VALUES (
            $1,$2,
            $3,$4,$5,$6,
            $7,$8,
            $9,$10,$11,
            $12,$13,
            $14,$15,$16,
            $17,$18,$19,$20,
            $21,$22
        )
        ON CONFLICT (user_id, channel_id, tg_msg_id) DO UPDATE SET
            msg_type      = EXCLUDED.msg_type,
            date          = EXCLUDED.date,
            edited        = EXCLUDED.edited,
            from_name     = EXCLUDED.from_name,
            from_id       = EXCLUDED.from_id,
            file          = EXCLUDED.file,
            file_name     = EXCLUDED.file_name,
            file_size     = EXCLUDED.file_size,
            width         = EXCLUDED.width,
            height        = EXCLUDED.height,
            -- Same album rule as videos: a silent sibling re-sync must not wipe
            -- the caption we propagated onto it.
            text          = COALESCE(NULLIF(EXCLUDED.text, ''), photos.text),
            text_entities = EXCLUDED.text_entities,
            grouped_id    = EXCLUDED.grouped_id,
            -- Keep a known locator when a later JSON re-import carries none.
            tg_photo_id    = CASE WHEN EXCLUDED.tg_photo_id > 0 THEN EXCLUDED.tg_photo_id ELSE photos.tg_photo_id END,
            access_hash    = CASE WHEN EXCLUDED.tg_photo_id > 0 THEN EXCLUDED.access_hash ELSE photos.access_hash END,
            file_reference = CASE WHEN EXCLUDED.tg_photo_id > 0 THEN EXCLUDED.file_reference ELSE photos.file_reference END,
            dc_id          = CASE WHEN EXCLUDED.dc_id > 0 THEN EXCLUDED.dc_id ELSE photos.dc_id END,
            size_type      = COALESCE(EXCLUDED.size_type, photos.size_type),
            thumb_size     = COALESCE(EXCLUDED.thumb_size, photos.thumb_size)
        RETURNING id
    `

func upsertPhotoArgs(p *Photo) []any {
	return []any{
		p.UserID, p.ChannelID,
		p.TGMsgID, nilIfEmpty(p.MsgType), p.Date, p.Edited,
		nilIfEmpty(p.FromName), nilIfEmpty(p.FromID),
		nilIfEmpty(p.File), nilIfEmpty(p.FileName), p.FileSize,
		p.Width, p.Height,
		nilIfEmpty(p.Text), p.TextEntities, nilIfZero64(p.GroupedID),
		p.TGPhotoID, p.AccessHash, p.FileReference, p.DCID,
		nilIfEmpty(p.SizeType), nilIfEmpty(p.ThumbSize),
	}
}

// UpsertPhoto writes one image row, idempotent on (user_id, channel_id,
// tg_msg_id) — a message carries at most one photo.
func (d *DB) UpsertPhoto(ctx context.Context, p *Photo) (int64, error) {
	row := d.QueryRow(ctx, upsertPhotoSQL, upsertPhotoArgs(p)...)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// UpsertPhotos writes a page of images in one round trip — see UpsertVideos for
// why that matters.
func (d *DB) UpsertPhotos(ctx context.Context, ps []*Photo) error {
	if len(ps) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, p := range ps {
		b.Queue(upsertPhotoSQL, upsertPhotoArgs(p)...)
	}
	return execBatch(ctx, d, b, len(ps))
}

// UpdatePhotoLocator persists a freshly resolved photo locator (first view of a
// JSON-imported row, or a FILE_REFERENCE_EXPIRED refresh).
func (d *DB) UpdatePhotoLocator(ctx context.Context, id int64, photoID, accessHash int64, fr []byte, dcID int, size int64, sizeType, thumbSize string) error {
	_, err := d.Exec(ctx, `
        UPDATE photos SET
            tg_photo_id    = $2,
            access_hash    = $3,
            file_reference = $4,
            dc_id          = CASE WHEN $5 > 0 THEN $5 ELSE dc_id END,
            file_size      = CASE WHEN $6 > 0 THEN $6 ELSE file_size END,
            size_type      = COALESCE(NULLIF($7, ''), size_type),
            thumb_size     = COALESCE(NULLIF($8, ''), thumb_size)
        WHERE id=$1
    `, id, photoID, accessHash, fr, dcID, size, sizeType, thumbSize)
	return err
}

// SetPhotoThumbPath records the on-disk thumbnail (relative to CACHE_DIR).
func (d *DB) SetPhotoThumbPath(ctx context.Context, id int64, path string) error {
	_, err := d.Exec(ctx, `UPDATE photos SET thumb_path=$2 WHERE id=$1`, id, nilIfEmpty(path))
	return err
}

const propagatePhotoCaptionSQL = `
        WITH cap AS (
            SELECT text, text_entities
            FROM photos
            WHERE user_id=$1 AND channel_id=$2 AND grouped_id=$3
              AND COALESCE(text, '') <> ''
            LIMIT 1
        )
        UPDATE photos p
        SET text = cap.text, text_entities = cap.text_entities
        FROM cap
        WHERE p.user_id=$1 AND p.channel_id=$2 AND p.grouped_id=$3
          AND COALESCE(p.text, '') = ''
    `

// PropagatePhotoGroupCaption is PropagateGroupCaption for images: an album's
// caption lives on exactly one member, copy it onto the silent siblings so a
// text search finds all of them.
func (d *DB) PropagatePhotoGroupCaption(ctx context.Context, userID, channelID, groupedID int64) error {
	if groupedID == 0 {
		return nil
	}
	_, err := d.Exec(ctx, propagatePhotoCaptionSQL, userID, channelID, groupedID)
	return err
}

func (d *DB) PhotoByID(ctx context.Context, id, userID int64) (*Photo, error) {
	row := d.QueryRow(ctx, `SELECT `+photoCols+` FROM photos p WHERE p.id=$1 AND p.user_id=$2`, id, userID)
	p, err := scanPhoto(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

// ListPhotosOpts mirrors ListVideosOpts (minus the streamer filter, which is a
// filename convention that only applies to videos).
type ListPhotosOpts struct {
	UserID    int64
	ChannelID int64
	Limit     int
	OffsetID  int64
	OrderBy   string
	FavOnly   bool
}

func (d *DB) ListPhotos(ctx context.Context, opt ListPhotosOpts) ([]Photo, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 200
	}
	q := `SELECT ` + photoCols + ` FROM photos p `
	args := []any{opt.UserID}
	where := []string{"p.user_id=$1"}
	if opt.ChannelID != 0 {
		args = append(args, opt.ChannelID)
		where = append(where, "p.channel_id=$"+itoa(len(args)))
	}
	if opt.OffsetID > 0 {
		args = append(args, opt.OffsetID)
		where = append(where, keysetCursorOn("photos", "p", opt.OrderBy, itoa(len(args)), false))
	}
	if opt.FavOnly {
		q += ` JOIN photo_favorites f ON f.photo_id=p.id AND f.user_id=p.user_id `
	}
	q += ` WHERE ` + joinWhere(where)
	q += orderClauseOn("p", opt.OrderBy, false)
	args = append(args, opt.Limit)
	q += ` LIMIT $` + itoa(len(args))
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// SearchPhotosOpts mirrors SearchVideosOpts. Photos rarely carry a file name,
// so FileName is matched but will usually be empty.
type SearchPhotosOpts struct {
	UserID    int64
	Q         string
	Text      string
	FileName  string
	DateFrom  *time.Time
	DateTo    *time.Time
	ChannelID int64
	Limit     int
	OffsetID  int64
	OrderBy   string
	FavOnly   bool
}

func (d *DB) SearchPhotos(ctx context.Context, opt SearchPhotosOpts) ([]Photo, error) {
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 200
	}
	args := []any{opt.UserID}
	where := []string{"p.user_id=$1"}
	if opt.Q != "" {
		args = append(args, "%"+opt.Q+"%")
		i := itoa(len(args))
		where = append(where, "(p.file_name ILIKE $"+i+" OR p.text ILIKE $"+i+")")
	}
	if opt.Text != "" {
		args = append(args, "%"+opt.Text+"%")
		where = append(where, "p.text ILIKE $"+itoa(len(args)))
	}
	if opt.FileName != "" {
		args = append(args, "%"+opt.FileName+"%")
		where = append(where, "p.file_name ILIKE $"+itoa(len(args)))
	}
	if opt.DateFrom != nil {
		args = append(args, *opt.DateFrom)
		where = append(where, "p.date >= $"+itoa(len(args)))
	}
	if opt.DateTo != nil {
		args = append(args, *opt.DateTo)
		where = append(where, "p.date <= $"+itoa(len(args)))
	}
	if opt.ChannelID != 0 {
		args = append(args, opt.ChannelID)
		where = append(where, "p.channel_id=$"+itoa(len(args)))
	}
	if opt.OffsetID > 0 {
		args = append(args, opt.OffsetID)
		where = append(where, keysetCursorOn("photos", "p", opt.OrderBy, itoa(len(args)), false))
	}
	args = append(args, opt.Limit)
	from := `FROM photos p `
	if opt.FavOnly {
		from += `JOIN photo_favorites f ON f.photo_id=p.id AND f.user_id=p.user_id `
	}
	q := `SELECT ` + photoCols + ` ` + from + `WHERE ` + joinWhere(where) +
		orderClauseOn("p", opt.OrderBy, false) + ` LIMIT $` + itoa(len(args))
	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (d *DB) CountPhotosByChannel(ctx context.Context, userID, channelID int64) (int64, error) {
	row := d.QueryRow(ctx, `SELECT count(*) FROM photos WHERE user_id=$1 AND channel_id=$2`, userID, channelID)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (d *DB) DeletePhotosByChannel(ctx context.Context, userID, channelID int64) (int64, error) {
	tag, err := d.Exec(ctx, `DELETE FROM photos WHERE user_id=$1 AND channel_id=$2`, userID, channelID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PhotoByIDAny looks a photo up without the user scope. Only for internal
// background work (the cache downloader), which already holds a row id that
// came from a user-scoped query — never reachable from a request path.
func (d *DB) PhotoByIDAny(ctx context.Context, id int64) (*Photo, error) {
	row := d.QueryRow(ctx, `SELECT `+photoCols+` FROM photos p WHERE p.id=$1`, id)
	p, err := scanPhoto(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}
