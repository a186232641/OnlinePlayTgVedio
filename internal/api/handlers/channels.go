package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/indexer"
)

type ChannelsHandlers struct {
	DB      *db.DB
	Indexer *indexer.Indexer
}

type channelDTO struct {
	ID              int64  `json:"id"`
	TGSessionID     int64  `json:"tg_session_id"`
	TGChannelID     int64  `json:"tg_channel_id"`
	Title           string `json:"title"`
	Username        string `json:"username,omitempty"`
	VideoCount      int    `json:"video_count"`
	PhotoCount      int    `json:"photo_count"`
	LastIndexedAt   string `json:"last_indexed_at,omitempty"`
	GroupByStreamer bool   `json:"group_by_streamer"`
	AutoSync        bool   `json:"auto_sync"`

	// Forum/topic shape. DialogKind is "channel" | "megagroup" | "topic";
	// IsForum marks a megagroup whose content lives in topics, and TopicCount is
	// how many of them we know about.
	DialogKind      string `json:"dialog_kind"`
	IsForum         bool   `json:"is_forum"`
	TopicCount      int64  `json:"topic_count"`
	// TopicVideoCount/TopicPhotoCount sum the media across a forum group's
	// topics (its own counters are 0 — the content lives one level down).
	TopicVideoCount int64 `json:"topic_video_count"`
	TopicPhotoCount int64 `json:"topic_photo_count"`
	TopicID         int32  `json:"topic_id,omitempty"`
	ParentChannelID int64  `json:"parent_channel_id,omitempty"`
	TopicClosed     bool   `json:"topic_closed,omitempty"`
	TopicsSyncedAt  string `json:"topics_synced_at,omitempty"`
}

func channelToDTO(c db.Channel) channelDTO {
	dto := channelDTO{
		ID:              c.ID,
		TGSessionID:     c.TGSessionID,
		TGChannelID:     c.TGChannelID,
		Title:           c.Title,
		Username:        c.Username,
		VideoCount:      c.VideoCount,
		PhotoCount:      c.PhotoCount,
		GroupByStreamer: c.GroupByStreamer,
		AutoSync:        c.AutoSync,
		DialogKind:      c.DialogKind,
		IsForum:         c.IsForum,
		TopicClosed:     c.TopicClosed,
	}
	if c.LastIndexedAt != nil {
		dto.LastIndexedAt = c.LastIndexedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if c.TopicsSyncedAt != nil {
		dto.TopicsSyncedAt = c.TopicsSyncedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if c.TopicID != nil {
		dto.TopicID = *c.TopicID
	}
	if c.ParentChannelID != nil {
		dto.ParentChannelID = *c.ParentChannelID
	}
	return dto
}

// Get returns a single channel (used by the channel detail page to read title
// and the group_by_streamer flag).
//
// GET /api/channels/:id
func (h *ChannelsHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	c, err := h.DB.ChannelByID(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
		return
	}
	dto := channelToDTO(*c)
	if c.IsForum {
		if n, err := h.DB.CountTopics(r.Context(), c.ID, uid); err == nil {
			dto.TopicCount = n
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"channel": dto})
}

// topicDTO is a topic's channel row plus its live sync state, so the topic list
// can show per-topic progress by polling ONE endpoint. Asking for each topic's
// status separately would mean a request per row on every page load.
type topicDTO struct {
	channelDTO
	Sync *indexer.SyncState `json:"sync,omitempty"`
}

// Topics lists a forum group's topics.
//
// GET /api/channels/:id/topics
func (h *ChannelsHandlers) Topics(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	if _, err := h.DB.ChannelByID(r.Context(), cid, uid); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
		return
	}
	topics, err := h.DB.ListTopics(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]topicDTO, 0, len(topics))
	for _, t := range topics {
		dto := topicDTO{channelDTO: channelToDTO(t)}
		if h.Indexer != nil {
			// Sync state is in-memory and per channel id; a topic is a channel
			// row, so this is the same lookup the detail page does.
			if st := h.Indexer.SyncStatus(t.ID); st.Running || !st.FinishedAt.IsZero() || st.LastError != "" {
				s := st
				dto.Sync = &s
			}
		}
		out = append(out, dto)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"topics": out})
}

// TopicsRefresh re-enumerates a forum group's topics from Telegram. Cheap
// (a couple of RPCs) and safe to repeat — it only upserts the topic rows.
//
// POST /api/channels/:id/topics/refresh
func (h *ChannelsHandlers) TopicsRefresh(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	if h.Indexer == nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "no_indexer", "indexer not wired"))
		return
	}
	n, err := h.Indexer.RefreshTopics(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "topics_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "topics": n})
}

// Backfill re-arms the full-history walk and starts a sync.
//
// Needed after a feature widens what sync stores (images, for instance): a
// channel that already finished its backfill has history_complete=TRUE, so a
// normal sync only pulls new messages at the top and the older images would
// never arrive.
//
// POST /api/channels/:id/backfill
func (h *ChannelsHandlers) Backfill(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	ch, err := h.DB.ChannelByID(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
		return
	}
	if h.Indexer == nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "no_indexer", "syncer not wired"))
		return
	}
	// A forum group holds no messages itself — re-arm each of its topics.
	targets := []int64{cid}
	if ch.IsForum {
		topics, err := h.DB.ListTopics(r.Context(), cid, uid)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		targets = targets[:0]
		for _, t := range topics {
			targets = append(targets, t.ID)
		}
	}
	for _, id := range targets {
		if err := h.DB.ResetHistoryComplete(r.Context(), id); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	st, err := h.Indexer.SyncStart(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}

// UpdateChannel patches mutable per-channel settings (currently just the
// group_by_streamer toggle). Only provided fields are applied.
//
// PATCH /api/channels/:id   body: {"group_by_streamer": true}
func (h *ChannelsHandlers) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	var body struct {
		GroupByStreamer *bool `json:"group_by_streamer"`
		AutoSync        *bool `json:"auto_sync"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_json", "invalid JSON body"))
		return
	}
	if body.GroupByStreamer != nil {
		if err := h.DB.SetChannelGrouping(r.Context(), cid, uid, *body.GroupByStreamer); err != nil {
			if err == db.ErrNotFound {
				httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
				return
			}
			httpx.WriteError(w, err)
			return
		}
	}
	if body.AutoSync != nil {
		if err := h.DB.SetChannelAutoSync(r.Context(), cid, uid, *body.AutoSync); err != nil {
			if err == db.ErrNotFound {
				httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
				return
			}
			httpx.WriteError(w, err)
			return
		}
	}
	c, err := h.DB.ChannelByID(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
		return
	}
	dto := channelToDTO(*c)
	if c.IsForum {
		if n, err := h.DB.CountTopics(r.Context(), c.ID, uid); err == nil {
			dto.TopicCount = n
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"channel": dto})
}

type streamerDTO struct {
	Streamer string `json:"streamer"` // "" = filenames not matching the pattern
	Count    int64  `json:"count"`
}

// Streamers returns the per-streamer video-count breakdown for a channel,
// busiest first. Used by the grouped channel view.
//
// GET /api/channels/:id/streamers
func (h *ChannelsHandlers) Streamers(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	if _, err := h.DB.ChannelByID(r.Context(), cid, uid); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
		return
	}
	rows, err := h.DB.ListStreamers(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]streamerDTO, 0, len(rows))
	for _, s := range rows {
		out = append(out, streamerDTO{Streamer: s.Streamer, Count: s.Count})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"streamers": out})
}

// List returns the current user's channels for browsing/management. Forum
// containers and forum topics are filtered out — the simplified UI doesn't
// expose them.
func (h *ChannelsHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	opt := db.ListChannelsOpts{UserID: uid}
	if v := r.URL.Query().Get("session_id"); v != "" {
		if sid, err := strconv.ParseInt(v, 10, 64); err == nil {
			opt.SessionID = sid
		}
	}
	chs, err := h.DB.ListChannels(r.Context(), opt)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// One grouped query instead of a COUNT per row.
	topicStats, err := h.DB.TopicStats(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]channelDTO, 0, len(chs))
	for _, c := range chs {
		// Only broadcast channels and supergroups are browsable at this level.
		// Topic rows are reached through their group's topic list instead, and
		// basic groups / private chats stay hidden.
		if c.DialogKind != db.DialogKindChannel && c.DialogKind != db.DialogKindMegagroup {
			continue
		}
		dto := channelToDTO(c)
		if st, ok := topicStats[c.ID]; ok {
			dto.TopicCount = st.Topics
			dto.TopicVideoCount = st.Videos
			dto.TopicPhotoCount = st.Photos
		}
		out = append(out, dto)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"channels": out})
}

// videoDTO mirrors the JSON-export field names (snake_case) so frontend can
// consume them without translation.
type videoDTO struct {
	ID              int64  `json:"id"`
	ChannelID       int64  `json:"channel_id"`
	TGMsgID         int64  `json:"tg_msg_id"`
	Date            string `json:"date,omitempty"`
	FromName        string `json:"from,omitempty"`
	FromID          string `json:"from_id,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	FileSize        int64  `json:"file_size"`
	MediaType       string `json:"media_type,omitempty"`
	MimeType        string `json:"mime_type,omitempty"`
	DurationSeconds int    `json:"duration_seconds"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	Text            string `json:"text"`
	StreamURL       string `json:"stream_url"`
}

func videoToDTO(v db.Video) videoDTO {
	d := videoDTO{
		ID:              v.ID,
		ChannelID:       v.ChannelID,
		TGMsgID:         v.TGMsgID,
		FromName:        v.FromName,
		FromID:          v.FromID,
		FileName:        v.FileName,
		FileSize:        v.FileSize,
		MediaType:       v.MediaType,
		MimeType:        v.MimeType,
		DurationSeconds: v.DurationSeconds,
		Width:           v.Width,
		Height:          v.Height,
		Text:            v.Text,
		StreamURL:       "/api/videos/" + strconv.FormatInt(v.ID, 10) + "/stream",
	}
	if v.Date != nil {
		d.Date = v.Date.Format("2006-01-02T15:04:05Z07:00")
	}
	return d
}

// SyncStart triggers an in-process goroutine that pulls messages.getHistory
// for the channel and upserts each video row. Idempotent: a second call while
// running returns the in-flight state.
//
// POST /api/channels/:id/sync
func (h *ChannelsHandlers) SyncStart(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	if h.Indexer == nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "no_indexer", "syncer not wired"))
		return
	}
	st, err := h.Indexer.SyncStart(r.Context(), cid, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}

// SyncStatus returns the current state of the sync for polling.
//
// GET /api/channels/:id/sync
func (h *ChannelsHandlers) SyncStatus(w http.ResponseWriter, r *http.Request) {
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	if h.Indexer == nil {
		httpx.WriteJSON(w, http.StatusOK, indexer.SyncState{})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.Indexer.SyncStatus(cid))
}

// ClearVideos removes every video row for a channel. Used before a fresh
// JSON import to drop stale data (and any orphan rows from earlier indexer
// experiments). Favorites cascade automatically.
//
// DELETE /api/channels/:id/videos
func (h *ChannelsHandlers) ClearVideos(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	if _, err := h.DB.ChannelByID(r.Context(), cid, uid); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
		return
	}
	n, err := h.DB.DeleteVideosByChannel(r.Context(), uid, cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	photos, err := h.DB.DeletePhotosByChannel(r.Context(), uid, cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// Re-arm the backfill: without this the channel keeps history_complete=TRUE
	// and the next sync skips Phase B entirely, so the wiped history never returns.
	_ = h.DB.ResetHistoryComplete(r.Context(), cid)
	_ = h.DB.MarkChannelIndexed(r.Context(), cid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": n + photos, "videos": n, "photos": photos})
}

func (h *ChannelsHandlers) ChannelVideos(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid channel id"))
		return
	}
	if _, err := h.DB.ChannelByID(r.Context(), cid, uid); err != nil {
		if err == db.ErrNotFound {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "channel not found"))
			return
		}
		httpx.WriteError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offsetID, _ := strconv.ParseInt(r.URL.Query().Get("offset_id"), 10, 64)
	order := r.URL.Query().Get("order")
	// streamer filter: present (even empty, = the NULL bucket) ⇒ filter on it.
	streamerFilter := r.URL.Query().Has("streamer")
	vids, err := h.DB.ListVideos(r.Context(), db.ListVideosOpts{
		UserID:         uid,
		ChannelID:      cid,
		Limit:          limit,
		OffsetID:       offsetID,
		OrderBy:        order,
		StreamerFilter: streamerFilter,
		Streamer:       r.URL.Query().Get("streamer"),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]videoDTO, 0, len(vids))
	for _, v := range vids {
		out = append(out, videoToDTO(v))
	}
	resp := map[string]any{"videos": out}
	// total only on first page, and only for the whole channel — under a streamer
	// filter the per-streamer count comes from the /streamers endpoint instead.
	if offsetID == 0 && !streamerFilter {
		if total, err := h.DB.CountVideosByChannel(r.Context(), uid, cid); err == nil {
			resp["total"] = total
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
