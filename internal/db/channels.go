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
	// DialogKindTopic is one forum topic, stored as a child row of its
	// megagroup. A forum group itself stays DialogKindMegagroup and carries
	// is_forum=TRUE.
	DialogKindTopic  = "topic"
	DialogKindGroup  = "group"
	DialogKindUser   = "user"
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

	// Forum/topic fields. IsForum marks a megagroup whose messages are split
	// into topics — it stays dialog_kind='megagroup' so existing list filters
	// keep working; the topics themselves are dialog_kind='topic' child rows
	// (parent_channel_id + topic_id, see migration 0002).
	IsForum          bool
	PhotoCount       int
	TopicsSyncedAt   *time.Time
	TopicIconColor   *int32
	TopicIconEmojiID *int64
	TopicClosed      bool
}

// All columns are qualified with the c.* alias because some queries
// (ListChannels) JOIN tg_sessions which also has id/user_id and would
// otherwise trigger "column reference ... is ambiguous" (SQLSTATE 42702).
const channelCols = `
    c.id, c.user_id, COALESCE(c.tg_session_id, 0), c.tg_channel_id, c.access_hash, c.title,
    COALESCE(c.username, ''), COALESCE(c.photo_path, ''),
    c.dialog_kind, c.parent_channel_id, c.topic_id,
    c.index_enabled, COALESCE(c.index_status, 'idle'), COALESCE(c.index_error, ''),
    c.video_count, c.last_indexed_at, c.group_by_streamer, c.history_complete, c.auto_sync,
    c.is_forum, c.photo_count, c.topics_synced_at,
    c.topic_icon_color, c.topic_icon_emoji_id, c.topic_closed
`

func scanChannel(row pgx.Row) (*Channel, error) {
	c := &Channel{}
	if err := row.Scan(
		&c.ID, &c.UserID, &c.TGSessionID, &c.TGChannelID, &c.AccessHash, &c.Title,
		&c.Username, &c.PhotoPath,
		&c.DialogKind, &c.ParentChannelID, &c.TopicID,
		&c.IndexEnabled, &c.IndexStatus, &c.IndexError,
		&c.VideoCount, &c.LastIndexedAt, &c.GroupByStreamer, &c.HistoryComplete, &c.AutoSync,
		&c.IsForum, &c.PhotoCount, &c.TopicsSyncedAt,
		&c.TopicIconColor, &c.TopicIconEmojiID, &c.TopicClosed,
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
            dialog_kind, parent_channel_id, topic_id,
            is_forum, topic_icon_color, topic_icon_emoji_id, topic_closed
        )
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
        ON CONFLICT (tg_session_id, tg_channel_id, COALESCE(topic_id, 0)) DO UPDATE SET
            access_hash         = EXCLUDED.access_hash,
            title               = EXCLUDED.title,
            username            = EXCLUDED.username,
            dialog_kind         = EXCLUDED.dialog_kind,
            parent_channel_id   = EXCLUDED.parent_channel_id,
            is_forum            = EXCLUDED.is_forum,
            topic_icon_color    = COALESCE(EXCLUDED.topic_icon_color, channels.topic_icon_color),
            topic_icon_emoji_id = COALESCE(EXCLUDED.topic_icon_emoji_id, channels.topic_icon_emoji_id),
            topic_closed        = EXCLUDED.topic_closed
        RETURNING id
    `,
		c.UserID, c.TGSessionID, c.TGChannelID, c.AccessHash, c.Title, nilIfEmpty(c.Username),
		c.DialogKind, c.ParentChannelID, c.TopicID,
		c.IsForum, c.TopicIconColor, c.TopicIconEmojiID, c.TopicClosed,
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
            photo_count = (SELECT count(*) FROM photos WHERE channel_id=$1),
            last_indexed_at=NOW(),
            index_status='idle', index_error=NULL
        WHERE id=$1
    `, channelID)
	return err
}


type ListChannelsOpts struct {
	UserID    int64
	SessionID int64 // 0 = all sessions for this user

	// Q filters by title (ILIKE). Limit > 0 turns on paging; 0 keeps the old
	// "everything" behaviour, which the home page and the search dropdown rely on.
	Q      string
	Limit  int
	Offset int
}

// ListChannels lists the browsable channels (broadcast channels and supergroups;
// topic rows, basic groups and private chats are excluded).
//
// The kind filter is in SQL rather than in the handler: with paging on, filtering
// after the LIMIT would hand out short pages and make "has more" lie.
//
// Paging here is limit/offset, deliberately not the offset_id keyset the media
// lists use. The sort key (last_indexed_at) moves whenever a sync finishes, so a
// keyset gains no correctness over an offset, and these lists are at most a few
// thousand rows — the case keyset exists for is million-row media tables.
func (d *DB) ListChannels(ctx context.Context, opt ListChannelsOpts) ([]Channel, bool, error) {
	q := `
        SELECT ` + channelCols + `
        FROM channels c
        JOIN tg_sessions s ON s.id = c.tg_session_id
        WHERE c.user_id=$1 AND s.status <> 'revoked'
          AND c.dialog_kind IN ($2, $3)
    `
	args := []any{opt.UserID, DialogKindChannel, DialogKindMegagroup}
	if opt.SessionID != 0 {
		args = append(args, opt.SessionID)
		q += ` AND c.tg_session_id=$` + itoa(len(args))
	}
	if opt.Q != "" {
		args = append(args, "%"+opt.Q+"%")
		q += ` AND c.title ILIKE $` + itoa(len(args))
	}
	q += ` ORDER BY c.last_indexed_at DESC NULLS LAST, c.title, c.id`
	q, args = appendPage(q, args, opt.Limit, opt.Offset)

	rows, err := d.Query(ctx, q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	out, more := trimPage(out, opt.Limit)
	return out, more, nil
}

// appendPage adds LIMIT/OFFSET for limit > 0, fetching one extra row so the
// caller can tell whether another page exists without a COUNT.
func appendPage(q string, args []any, limit, offset int) (string, []any) {
	if limit <= 0 {
		return q, args
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit+1, offset)
	return q + ` LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args)), args
}

// trimPage drops the look-ahead row appendPage asked for and reports whether
// it was there.
func trimPage[T any](rows []T, limit int) ([]T, bool) {
	if limit <= 0 {
		return rows, false
	}
	if limit > 200 {
		limit = 200
	}
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
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
//
// q matches the streamer name; the NULL bucket (filenames not matching the
// pattern) is shown as "其它" in the UI, so a search for that word finds it too.
// Paging is limit/offset: these are GROUP BY rows with no id to key on.
func (d *DB) ListStreamers(ctx context.Context, channelID, userID int64, q string, limit, offset int) ([]StreamerCount, bool, error) {
	sql := `
        SELECT COALESCE(streamer, ''), count(*)
        FROM videos
        WHERE channel_id=$1 AND user_id=$2
    `
	args := []any{channelID, userID}
	if q != "" {
		args = append(args, "%"+q+"%")
		n := itoa(len(args))
		sql += ` AND (streamer ILIKE $` + n + ` OR (streamer IS NULL AND '其它' ILIKE $` + n + `))`
	}
	sql += ` GROUP BY streamer ORDER BY count(*) DESC, COALESCE(streamer, '')`
	sql, args = appendPage(sql, args, limit, offset)

	rows, err := d.Query(ctx, sql, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []StreamerCount
	for rows.Next() {
		var s StreamerCount
		if err := rows.Scan(&s.Streamer, &s.Count); err != nil {
			return nil, false, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	out, more := trimPage(out, limit)
	return out, more, nil
}

// CountStreamers is the number of streamer buckets (matching q) in a channel —
// the badge total, asked only on a list's first page.
func (d *DB) CountStreamers(ctx context.Context, channelID, userID int64, q string) (int64, error) {
	sql := `SELECT count(*) FROM (SELECT 1 FROM videos WHERE channel_id=$1 AND user_id=$2`
	args := []any{channelID, userID}
	if q != "" {
		args = append(args, "%"+q+"%")
		sql += ` AND (streamer ILIKE $3 OR (streamer IS NULL AND '其它' ILIKE $3))`
	}
	sql += ` GROUP BY streamer) t`
	var n int64
	err := d.QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
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
// ListTopics returns the forum topics of one megagroup (its dialog_kind='topic'
// child rows), most recently active first. Topic rows carry the PARENT's
// tg_channel_id and access_hash, so every peer-building/refresh path keeps
// working on them unchanged.
func (d *DB) ListTopics(ctx context.Context, parentID, userID int64) ([]Channel, error) {
	rows, err := d.Query(ctx, `
        SELECT `+channelCols+`
        FROM channels c
        WHERE c.parent_channel_id=$1 AND c.user_id=$2 AND c.dialog_kind=$3
        ORDER BY c.last_indexed_at DESC NULLS LAST, c.topic_id
    `, parentID, userID, DialogKindTopic)
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

// ListTopicsPage is the browsing version of ListTopics: title search plus
// limit/offset paging, topics holding the most media first (a forum can have
// thousands of topics, and loading every one up front is what made the page
// crawl on a phone). Unsynced topics trail, newest first.
func (d *DB) ListTopicsPage(ctx context.Context, parentID, userID int64, q string, limit, offset int) ([]Channel, bool, error) {
	sql := `
        SELECT ` + channelCols + `
        FROM channels c
        WHERE c.parent_channel_id=$1 AND c.user_id=$2 AND c.dialog_kind=$3
    `
	args := []any{parentID, userID, DialogKindTopic}
	if q != "" {
		args = append(args, "%"+q+"%")
		sql += ` AND c.title ILIKE $` + itoa(len(args))
	}
	sql += ` ORDER BY (c.video_count + c.photo_count) DESC, c.id DESC`
	sql, args = appendPage(sql, args, limit, offset)

	rows, err := d.Query(ctx, sql, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	out, more := trimPage(out, limit)
	return out, more, nil
}

// CountTopicsMatching is CountTopics with the same title filter as
// ListTopicsPage, for the first-page total.
func (d *DB) CountTopicsMatching(ctx context.Context, parentID, userID int64, q string) (int64, error) {
	sql := `SELECT count(*) FROM channels WHERE parent_channel_id=$1 AND user_id=$2 AND dialog_kind=$3`
	args := []any{parentID, userID, DialogKindTopic}
	if q != "" {
		args = append(args, "%"+q+"%")
		sql += ` AND title ILIKE $4`
	}
	var n int64
	err := d.QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
}

// CountChannels is the first-page total for a filtered ListChannels.
func (d *DB) CountChannels(ctx context.Context, opt ListChannelsOpts) (int64, error) {
	sql := `
        SELECT count(*) FROM channels c
        JOIN tg_sessions s ON s.id = c.tg_session_id
        WHERE c.user_id=$1 AND s.status <> 'revoked' AND c.dialog_kind IN ($2, $3)
    `
	args := []any{opt.UserID, DialogKindChannel, DialogKindMegagroup}
	if opt.SessionID != 0 {
		args = append(args, opt.SessionID)
		sql += ` AND c.tg_session_id=$` + itoa(len(args))
	}
	if opt.Q != "" {
		args = append(args, "%"+opt.Q+"%")
		sql += ` AND c.title ILIKE $` + itoa(len(args))
	}
	var n int64
	err := d.QueryRow(ctx, sql, args...).Scan(&n)
	return n, err
}

// OwnedChannelIDs returns the subset of ids that belong to the user. Used to
// scope the batch sync-status endpoint, whose data lives in memory keyed only
// by channel id.
func (d *DB) OwnedChannelIDs(ctx context.Context, userID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.Query(ctx, `SELECT id FROM channels WHERE user_id=$1 AND id = ANY($2)`, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetTopicsSynced stamps when a forum group's topic list was last enumerated,
// and marks the group as a forum so the UI offers the topic drill-down.
func (d *DB) SetTopicsSynced(ctx context.Context, channelID int64) error {
	_, err := d.Exec(ctx, `UPDATE channels SET topics_synced_at=NOW(), is_forum=TRUE WHERE id=$1`, channelID)
	return err
}

// CountTopics is the number of known topics under a forum group.
func (d *DB) CountTopics(ctx context.Context, parentID, userID int64) (int64, error) {
	var n int64
	err := d.QueryRow(ctx, `
        SELECT count(*) FROM channels
        WHERE parent_channel_id=$1 AND user_id=$2 AND dialog_kind=$3
    `, parentID, userID, DialogKindTopic).Scan(&n)
	return n, err
}

// TopicStat aggregates a forum group's topics: how many there are, and how much
// media they hold between them.
type TopicStat struct {
	Topics int64
	Videos int64
	Photos int64
}

// TopicStats returns per-parent topic aggregates for one user in a single
// grouped query, so the channel list can show a forum group's real content
// without a query per row.
//
// The media totals are what decide whether a group belongs in the browsing
// view at all. Topic *count* is not enough: discovery enumerates the topics of
// every forum the account has joined, synced or not, so "has topics" is true
// for groups nobody has ever pulled a single message from.
func (d *DB) TopicStats(ctx context.Context, userID int64) (map[int64]TopicStat, error) {
	rows, err := d.Query(ctx, `
        SELECT parent_channel_id, count(*),
               COALESCE(SUM(video_count), 0), COALESCE(SUM(photo_count), 0)
        FROM channels
        WHERE user_id=$1 AND dialog_kind=$2 AND parent_channel_id IS NOT NULL
        GROUP BY parent_channel_id
    `, userID, DialogKindTopic)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]TopicStat{}
	for rows.Next() {
		var parent int64
		var st TopicStat
		if err := rows.Scan(&parent, &st.Topics, &st.Videos, &st.Photos); err != nil {
			return nil, err
		}
		out[parent] = st
	}
	return out, rows.Err()
}

// ChannelSource is the display identity of a channel row: its own title, and
// for a topic the group it belongs to.
type ChannelSource struct {
	ID          int64
	Title       string
	DialogKind  string
	ParentID    int64
	ParentTitle string
}

// ChannelSources resolves the given channel ids (scoped to the user) to their
// titles in one query, including the parent group for topic rows. Used by
// cross-channel lists — favorites — to label and link each item's origin.
func (d *DB) ChannelSources(ctx context.Context, userID int64, ids []int64) (map[int64]ChannelSource, error) {
	out := map[int64]ChannelSource{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := d.Query(ctx, `
        SELECT c.id, c.title, c.dialog_kind,
               COALESCE(c.parent_channel_id, 0), COALESCE(p.title, '')
        FROM channels c
        LEFT JOIN channels p ON p.id = c.parent_channel_id
        WHERE c.user_id=$1 AND c.id = ANY($2)
    `, userID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var src ChannelSource
		if err := rows.Scan(&src.ID, &src.Title, &src.DialogKind, &src.ParentID, &src.ParentTitle); err != nil {
			return nil, err
		}
		out[src.ID] = src
	}
	return out, rows.Err()
}

// SetIsForum records whether a megagroup is a forum (has topics). Discovery
// sets it, but sync re-checks it: getting it wrong is expensive, because a
// forum's own getHistory returns every topic's messages flattened into the
// group row.
func (d *DB) SetIsForum(ctx context.Context, channelID int64, isForum bool) error {
	_, err := d.Exec(ctx, `UPDATE channels SET is_forum=$2 WHERE id=$1`, channelID, isForum)
	return err
}
