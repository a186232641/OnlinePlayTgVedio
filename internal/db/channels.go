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
	GroupByStreamer bool
	HistoryComplete bool
	AutoSync        bool
}

// All columns are qualified with the c.* alias because some queries
// (ListChannels) JOIN tg_sessions which also has id/user_id and would
// otherwise trigger "column reference ... is ambiguous" (SQLSTATE 42702).
const channelCols = `
    c.id, c.user_id, COALESCE(c.tg_session_id, 0), c.tg_channel_id, c.access_hash, c.title,
    COALESCE(c.username, ''), COALESCE(c.photo_path, ''),
    c.dialog_kind, c.parent_channel_id, c.topic_id,
    c.index_enabled, COALESCE(c.index_status, 'idle'), COALESCE(c.index_error, ''),
    c.video_count, c.last_indexed_at, c.group_by_streamer, c.history_complete, c.auto_sync
`

func scanChannel(row pgx.Row) (*Channel, error) {
	c := &Channel{}
	if err := row.Scan(
		&c.ID, &c.UserID, &c.TGSessionID, &c.TGChannelID, &c.AccessHash, &c.Title,
		&c.Username, &c.PhotoPath,
		&c.DialogKind, &c.ParentChannelID, &c.TopicID,
		&c.IndexEnabled, &c.IndexStatus, &c.IndexError,
		&c.VideoCount, &c.LastIndexedAt, &c.GroupByStreamer, &c.HistoryComplete, &c.AutoSync,
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

// MarkChannelIndexed stamps last_indexed_at and recomputes video_count from the
// actual videos rows. It must NOT be passed a per-run delta: an incremental sync
// imports only the new messages, so writing that delta would clobber the real
// total. Recounting keeps the number correct after full imports, incremental
// syncs, and clears alike.
func (d *DB) MarkChannelIndexed(ctx context.Context, channelID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE channels
        SET video_count = (SELECT count(*) FROM videos WHERE channel_id=$1),
            last_indexed_at=NOW(),
            index_status='idle', index_error=NULL
        WHERE id=$1
    `, channelID)
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

// ChannelsForAutoSync lists every channel eligible for the background sweep:
// synced/imported at least once (last_indexed_at IS NOT NULL), auto_sync still
// on, session not revoked. Oldest-indexed first so the most stale refreshes
// earliest. Spans all users.
func (d *DB) ChannelsForAutoSync(ctx context.Context) ([]AutoSyncRef, error) {
	rows, err := d.Query(ctx, `
        SELECT c.id, c.user_id
        FROM channels c
        JOIN tg_sessions s ON s.id = c.tg_session_id
        WHERE c.last_indexed_at IS NOT NULL AND c.auto_sync AND s.status <> 'revoked'
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

// SetChannelGrouping toggles the per-channel "group by streamer" flag. Scoped
// by user so one user can't flip another's channel. Returns ErrNotFound if no
// such channel for this user.
func (d *DB) SetChannelGrouping(ctx context.Context, channelID, userID int64, enabled bool) error {
	tag, err := d.Exec(ctx, `
        UPDATE channels SET group_by_streamer=$3 WHERE id=$1 AND user_id=$2
    `, channelID, userID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetChannelAutoSync toggles whether the background scheduler includes this
// channel. Scoped by user. Returns ErrNotFound if no such channel for the user.
func (d *DB) SetChannelAutoSync(ctx context.Context, channelID, userID int64, enabled bool) error {
	tag, err := d.Exec(ctx, `
        UPDATE channels SET auto_sync=$3 WHERE id=$1 AND user_id=$2
    `, channelID, userID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetHistoryComplete marks a channel as fully backfilled (walked to its oldest
// message). Subsequent syncs then only pull new messages at the top.
func (d *DB) SetHistoryComplete(ctx context.Context, channelID int64) error {
	_, err := d.Exec(ctx, `UPDATE channels SET history_complete=TRUE WHERE id=$1`, channelID)
	return err
}

// ResetHistoryComplete clears the backfill-done flag so the next sync re-walks
// the full history. Used after clearing a channel's videos: otherwise sync would
// see history_complete=TRUE and skip the backfill, leaving the wiped channel empty.
func (d *DB) ResetHistoryComplete(ctx context.Context, channelID int64) error {
	_, err := d.Exec(ctx, `UPDATE channels SET history_complete=FALSE WHERE id=$1`, channelID)
	return err
}

// StreamerCount is one row of the per-channel streamer breakdown. Streamer is
// "" for videos whose filename doesn't match the "{streamer}-DATE" pattern.
type StreamerCount struct {
	Streamer string
	Count    int64
}

// ListStreamers returns the distinct streamers in a channel with their video
// counts, busiest first. Backed by idx_videos_channel_streamer.
func (d *DB) ListStreamers(ctx context.Context, channelID, userID int64) ([]StreamerCount, error) {
	rows, err := d.Query(ctx, `
        SELECT COALESCE(streamer, ''), count(*)
        FROM videos
        WHERE channel_id=$1 AND user_id=$2
        GROUP BY streamer
        ORDER BY count(*) DESC, COALESCE(streamer, '')
    `, channelID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StreamerCount
	for rows.Next() {
		var s StreamerCount
		if err := rows.Scan(&s.Streamer, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
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