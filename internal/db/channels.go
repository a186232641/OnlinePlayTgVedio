package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	DialogKindChannel   = "channel"
	DialogKindMegagroup = "megagroup"
	DialogKindForum     = "forum" // megagroup with forum=true; only its topics are indexable
	DialogKindTopic     = "topic"
	DialogKindGroup     = "group"
	DialogKindUser      = "user"
)

const (
	IndexStatusIdle    = "idle"
	IndexStatusQueued  = "queued"
	IndexStatusRunning = "running"
	IndexStatusFailed  = "failed"
)

type Channel struct {
	ID              int64
	UserID          int64
	TGSessionID     int64
	TGChannelID     int64
	AccessHash      int64
	Title           string
	Username        string
	PhotoPath       string
	DialogKind      string
	ParentChannelID *int64
	TopicID         *int32
	IndexEnabled    bool
	IndexStatus     string
	IndexError      string
	VideoCount      int
	LastIndexedAt   *time.Time
}

// All columns are qualified with the c.* alias because some queries
// (ListChannels) JOIN tg_sessions which also has id/user_id and would
// otherwise trigger "column reference ... is ambiguous" (SQLSTATE 42702).
const channelCols = `
    c.id, c.user_id, COALESCE(c.tg_session_id, 0), c.tg_channel_id, c.access_hash, c.title,
    COALESCE(c.username, ''), COALESCE(c.photo_path, ''),
    c.dialog_kind, c.parent_channel_id, c.topic_id,
    c.index_enabled, COALESCE(c.index_status, 'idle'), COALESCE(c.index_error, ''),
    c.video_count, c.last_indexed_at
`

func scanChannel(row pgx.Row) (*Channel, error) {
	c := &Channel{}
	if err := row.Scan(
		&c.ID, &c.UserID, &c.TGSessionID, &c.TGChannelID, &c.AccessHash, &c.Title,
		&c.Username, &c.PhotoPath,
		&c.DialogKind, &c.ParentChannelID, &c.TopicID,
		&c.IndexEnabled, &c.IndexStatus, &c.IndexError,
		&c.VideoCount, &c.LastIndexedAt,
	); err != nil {
		return nil, err
	}
	return c, nil
}

// UpsertChannel insert-or-updates a channel row keyed by
// (tg_session_id, tg_channel_id, COALESCE(topic_id,0)). Existing
// index_enabled/status are preserved on update — only metadata refreshes.
func (d *DB) UpsertChannel(ctx context.Context, c *Channel) (int64, error) {
	row := d.QueryRow(ctx, `
        INSERT INTO channels (
            user_id, tg_session_id, tg_channel_id, access_hash, title, username,
            dialog_kind, parent_channel_id, topic_id
        )
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
        ON CONFLICT (tg_session_id, tg_channel_id, COALESCE(topic_id, 0)) DO UPDATE SET
            access_hash       = EXCLUDED.access_hash,
            title             = EXCLUDED.title,
            username          = EXCLUDED.username,
            dialog_kind       = EXCLUDED.dialog_kind,
            parent_channel_id = EXCLUDED.parent_channel_id
        RETURNING id
    `,
		c.UserID, c.TGSessionID, c.TGChannelID, c.AccessHash, c.Title, nilIfEmpty(c.Username),
		c.DialogKind, c.ParentChannelID, c.TopicID,
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) MarkChannelIndexed(ctx context.Context, channelID int64, videoCount int) error {
	_, err := d.Exec(ctx, `
        UPDATE channels
        SET video_count=$1, last_indexed_at=NOW(),
            index_status='idle', index_error=NULL
        WHERE id=$2
    `, videoCount, channelID)
	return err
}


type ListChannelsOpts struct {
	UserID    int64
	SessionID int64 // 0 = all sessions for this user
}

func (d *DB) ListChannels(ctx context.Context, opt ListChannelsOpts) ([]Channel, error) {
	q := `
        SELECT ` + channelCols + `
        FROM channels c
        JOIN tg_sessions s ON s.id = c.tg_session_id
        WHERE c.user_id=$1 AND s.status <> 'revoked'
    `
	args := []any{opt.UserID}
	if opt.SessionID != 0 {
		args = append(args, opt.SessionID)
		q += ` AND c.tg_session_id=$2`
	}
	q += ` ORDER BY c.last_indexed_at DESC NULLS LAST, c.title`

	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// AutoSyncRef is the minimal handle the scheduler needs to call SyncStart.
type AutoSyncRef struct {
	ChannelID int64
	UserID    int64
}

// ChannelsForAutoSync lists every channel that has been synced/imported at
// least once (last_indexed_at IS NOT NULL) whose session is not revoked, oldest
// first so the most stale gets refreshed earliest. Spans all users.
func (d *DB) ChannelsForAutoSync(ctx context.Context) ([]AutoSyncRef, error) {
	rows, err := d.Query(ctx, `
        SELECT c.id, c.user_id
        FROM channels c
        JOIN tg_sessions s ON s.id = c.tg_session_id
        WHERE c.last_indexed_at IS NOT NULL AND s.status <> 'revoked'
        ORDER BY c.last_indexed_at ASC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutoSyncRef
	for rows.Next() {
		var ref AutoSyncRef
		if err := rows.Scan(&ref.ChannelID, &ref.UserID); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (d *DB) ChannelByID(ctx context.Context, id, userID int64) (*Channel, error) {
	row := d.QueryRow(ctx, `
        SELECT `+channelCols+`
        FROM channels c
        WHERE c.id=$1 AND c.user_id=$2
    `, id, userID)
	c, err := scanChannel(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (d *DB) UpdateChannelPhoto(ctx context.Context, channelID int64, path string) error {
	_, err := d.Exec(ctx, `UPDATE channels SET photo_path=$2 WHERE id=$1`, channelID, path)
	return err
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}