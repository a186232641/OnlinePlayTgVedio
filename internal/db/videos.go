package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Video struct {
	ID            int64
	UserID        int64
	ChannelID     int64
	TGMessageID   int64
	TGDocID       int64
	AccessHash    int64
	FileReference []byte
	Mime          string
	SizeBytes     int64
	DurationSec   int
	Width         int
	Height        int
	Caption       string
	SentAt        *time.Time
	ThumbPath     string
}

func (d *DB) UpsertVideo(ctx context.Context, v *Video) (int64, error) {
	row := d.QueryRow(ctx, `
        INSERT INTO videos (
            user_id, channel_id, tg_message_id, tg_doc_id, access_hash, file_reference,
            mime, size_bytes, duration_sec, width, height, caption, sent_at, thumb_path
        ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
        ON CONFLICT (user_id, channel_id, tg_message_id) DO UPDATE SET
            tg_doc_id      = EXCLUDED.tg_doc_id,
            access_hash    = EXCLUDED.access_hash,
            file_reference = EXCLUDED.file_reference,
            mime           = EXCLUDED.mime,
            size_bytes     = EXCLUDED.size_bytes,
            duration_sec   = EXCLUDED.duration_sec,
            width          = EXCLUDED.width,
            height         = EXCLUDED.height,
            caption        = EXCLUDED.caption,
            sent_at        = EXCLUDED.sent_at,
            thumb_path     = COALESCE(NULLIF(EXCLUDED.thumb_path,''), videos.thumb_path)
        RETURNING id
    `,
		v.UserID, v.ChannelID, v.TGMessageID, v.TGDocID, v.AccessHash, v.FileReference,
		nilIfEmpty(v.Mime), v.SizeBytes, v.DurationSec, v.Width, v.Height,
		nilIfEmpty(v.Caption), v.SentAt, nilIfEmpty(v.ThumbPath),
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) UpdateVideoFileReference(ctx context.Context, id int64, fr []byte) error {
	_, err := d.Exec(ctx, `UPDATE videos SET file_reference=$1 WHERE id=$2`, fr, id)
	return err
}

// UpdateVideoLocator persists the full Telegram doc locator. Used after
// JSON-imported placeholder rows get their first refresh, populating
// tg_doc_id + access_hash + file_reference together.
func (d *DB) UpdateVideoLocator(ctx context.Context, id int64, tgDocID, accessHash int64, fr []byte) error {
	_, err := d.Exec(ctx, `
        UPDATE videos SET tg_doc_id=$2, access_hash=$3, file_reference=$4
        WHERE id=$1
    `, id, tgDocID, accessHash, fr)
	return err
}

func (d *DB) UpdateVideoThumb(ctx context.Context, id int64, path string) error {
	_, err := d.Exec(ctx, `UPDATE videos SET thumb_path=$1 WHERE id=$2`, path, id)
	return err
}

type ListVideosOpts struct {
	UserID     int64
	ChannelID  int64 // 0 = all
	Limit      int
	OffsetID   int64 // pagination by id (descending)
	OrderBy    string // "sent_at" (default) | "duration"
	FavOnly    bool
}

func (d *DB) ListVideos(ctx context.Context, opt ListVideosOpts) ([]Video, error) {
	if opt.Limit <= 0 || opt.Limit > 200 {
		opt.Limit = 60
	}
	q := `
        SELECT v.id, v.user_id, v.channel_id, v.tg_message_id, v.tg_doc_id, v.access_hash,
               v.file_reference, COALESCE(v.mime,''), v.size_bytes, v.duration_sec,
               v.width, v.height, COALESCE(v.caption,''), v.sent_at, COALESCE(v.thumb_path,'')
        FROM videos v
    `
	args := []any{opt.UserID}
	where := []string{"v.user_id=$1"}
	if opt.ChannelID != 0 {
		args = append(args, opt.ChannelID)
		where = append(where, "v.channel_id=$2")
	}
	if opt.FavOnly {
		q += ` JOIN favorites f ON f.video_id=v.id AND f.user_id=v.user_id `
	}
	q += ` WHERE ` + joinWhere(where)
	if opt.OrderBy == "duration" {
		q += ` ORDER BY v.duration_sec DESC, v.id DESC`
	} else {
		q += ` ORDER BY v.sent_at DESC NULLS LAST, v.id DESC`
	}
	args = append(args, opt.Limit)
	q += ` LIMIT $` + itoa(len(args))

	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		var v Video
		if err := rows.Scan(&v.ID, &v.UserID, &v.ChannelID, &v.TGMessageID, &v.TGDocID, &v.AccessHash,
			&v.FileReference, &v.Mime, &v.SizeBytes, &v.DurationSec, &v.Width, &v.Height,
			&v.Caption, &v.SentAt, &v.ThumbPath); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) VideoByID(ctx context.Context, id, userID int64) (*Video, error) {
	row := d.QueryRow(ctx, `
        SELECT id, user_id, channel_id, tg_message_id, tg_doc_id, access_hash, file_reference,
               COALESCE(mime,''), size_bytes, duration_sec, width, height,
               COALESCE(caption,''), sent_at, COALESCE(thumb_path,'')
        FROM videos WHERE id=$1 AND user_id=$2
    `, id, userID)
	v := &Video{}
	if err := row.Scan(&v.ID, &v.UserID, &v.ChannelID, &v.TGMessageID, &v.TGDocID, &v.AccessHash,
		&v.FileReference, &v.Mime, &v.SizeBytes, &v.DurationSec, &v.Width, &v.Height,
		&v.Caption, &v.SentAt, &v.ThumbPath); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return v, nil
}

func (d *DB) SearchVideos(ctx context.Context, userID int64, query string, limit int) ([]Video, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := d.Query(ctx, `
        SELECT id, user_id, channel_id, tg_message_id, tg_doc_id, access_hash, file_reference,
               COALESCE(mime,''), size_bytes, duration_sec, width, height,
               COALESCE(caption,''), sent_at, COALESCE(thumb_path,'')
        FROM videos
        WHERE user_id=$1 AND caption_tsv @@ plainto_tsquery('simple', $2)
        ORDER BY ts_rank(caption_tsv, plainto_tsquery('simple', $2)) DESC,
                 sent_at DESC NULLS LAST
        LIMIT $3
    `, userID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Video
	for rows.Next() {
		var v Video
		if err := rows.Scan(&v.ID, &v.UserID, &v.ChannelID, &v.TGMessageID, &v.TGDocID, &v.AccessHash,
			&v.FileReference, &v.Mime, &v.SizeBytes, &v.DurationSec, &v.Width, &v.Height,
			&v.Caption, &v.SentAt, &v.ThumbPath); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func joinWhere(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
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
