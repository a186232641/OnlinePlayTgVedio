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
	ID              int64  `json:"id"`
	TGSessionID     int64  `json:"tg_session_id"`
	TGChannelID     int64  `json:"tg_channel_id"`
	Title           string `json:"title"`
	Username        string `json:"username,omitempty"`
	DialogKind      string `json:"dialog_kind"`
	ParentChannelID *int64 `json:"parent_channel_id,omitempty"`
	TopicID         *int32 `json:"topic_id,omitempty"`
	IndexEnabled    bool   `json:"index_enabled"`
	IndexStatus     string `json:"index_status"`
	IndexError      string `json:"index_error,omitempty"`
	VideoCount      int    `json:"video_count"`
	LastIndexedAt   string `json:"last_indexed_at,omitempty"`
}

func channelToDTO(c db.Channel) channelDTO {
	dto := channelDTO{
		ID:              c.ID,
		TGSessionID:     c.TGSessionID,
		TGChannelID:     c.TGChannelID,
		Title:           c.Title,
		Username:        c.Username,
		DialogKind:      c.DialogKind,
		ParentChannelID: c.ParentChannelID,
		TopicID:         c.TopicID,
		IndexEnabled:    c.IndexEnabled,
		IndexStatus:     c.IndexStatus,
		IndexError:      c.IndexError,
		VideoCount:      c.VideoCount,
	}
	if c.LastIndexedAt != nil {
		dto.LastIndexedAt = c.LastIndexedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return dto
}

// List returns the current user's channels, optionally filtered by ?session_id
// and/or ?enabled=true.
func (h *ChannelsHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	opt := db.ListChannelsOpts{UserID: uid}
	if v := r.URL.Query().Get("session_id"); v != "" {
		if sid, err := strconv.ParseInt(v, 10, 64); err == nil {
			opt.SessionID = sid
		}
	}
	if r.URL.Query().Get("enabled") == "true" {
		opt.OnlyEnabled = true
	}
	chs, err := h.DB.ListChannels(r.Context(), opt)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]channelDTO, 0, len(chs))
	for _, c := range chs {
		out = append(out, channelToDTO(c))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"channels": out})
}

type videoDTO struct {
	ID          int64  `json:"id"`
	ChannelID   int64  `json:"channel_id"`
	Caption     string `json:"caption"`
	DurationSec int    `json:"duration_sec"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SizeBytes   int64  `json:"size_bytes"`
	Mime        string `json:"mime"`
	SentAt      string `json:"sent_at,omitempty"`
	ThumbURL    string `json:"thumb_url,omitempty"`
	StreamURL   string `json:"stream_url"`
}

func videoToDTO(v db.Video) videoDTO {
	d := videoDTO{
		ID:          v.ID,
		ChannelID:   v.ChannelID,
		Caption:     v.Caption,
		DurationSec: v.DurationSec,
		Width:       v.Width,
		Height:      v.Height,
		SizeBytes:   v.SizeBytes,
		Mime:        v.Mime,
		StreamURL:   "/api/videos/" + strconv.FormatInt(v.ID, 10) + "/stream",
	}
	if v.SentAt != nil {
		d.SentAt = v.SentAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if v.ThumbPath != "" {
		d.ThumbURL = "/api/videos/" + strconv.FormatInt(v.ID, 10) + "/thumb"
	}
	return d
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
	order := r.URL.Query().Get("order")
	vids, err := h.DB.ListVideos(r.Context(), db.ListVideosOpts{
		UserID:    uid,
		ChannelID: cid,
		Limit:     limit,
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"videos": out})
}

// EnableIndex flips index_enabled=true and nudges the worker.
func (h *ChannelsHandlers) EnableIndex(w http.ResponseWriter, r *http.Request) {
	h.toggleIndex(w, r, true)
}

// DisableIndex flips index_enabled=false. Already-indexed videos stay.
func (h *ChannelsHandlers) DisableIndex(w http.ResponseWriter, r *http.Request) {
	h.toggleIndex(w, r, false)
}

func (h *ChannelsHandlers) toggleIndex(w http.ResponseWriter, r *http.Request, enabled bool) {
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
	if ch.DialogKind == db.DialogKindForum {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "forum_root", "forum container itself is not indexable; toggle individual topics"))
		return
	}
	if err := h.DB.SetChannelIndexEnabled(r.Context(), cid, uid, enabled); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if enabled && h.Indexer != nil {
		h.Indexer.NudgeWorker(ch.TGSessionID)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": enabled})
}
