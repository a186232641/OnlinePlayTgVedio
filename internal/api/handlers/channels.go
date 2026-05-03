package handlers

import (
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
	ID            int64  `json:"id"`
	TGSessionID   int64  `json:"tg_session_id"`
	TGChannelID   int64  `json:"tg_channel_id"`
	Title         string `json:"title"`
	Username      string `json:"username,omitempty"`
	VideoCount    int    `json:"video_count"`
	LastIndexedAt string `json:"last_indexed_at,omitempty"`
}

func channelToDTO(c db.Channel) channelDTO {
	dto := channelDTO{
		ID:          c.ID,
		TGSessionID: c.TGSessionID,
		TGChannelID: c.TGChannelID,
		Title:       c.Title,
		Username:    c.Username,
		VideoCount:  c.VideoCount,
	}
	if c.LastIndexedAt != nil {
		dto.LastIndexedAt = c.LastIndexedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
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
	out := make([]channelDTO, 0, len(chs))
	for _, c := range chs {
		if c.DialogKind == db.DialogKindForum || c.DialogKind == db.DialogKindTopic {
			continue
		}
		out = append(out, channelToDTO(c))
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
	_ = h.DB.MarkChannelIndexed(r.Context(), cid, 0)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": n})
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
	vids, err := h.DB.ListVideos(r.Context(), db.ListVideosOpts{
		UserID:    uid,
		ChannelID: cid,
		Limit:     limit,
		OffsetID:  offsetID,
		OrderBy:   order,
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
	// total only on first page (cheap on followups too but pointless to recompute)
	if offsetID == 0 {
		if total, err := h.DB.CountVideosByChannel(r.Context(), uid, cid); err == nil {
			resp["total"] = total
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
