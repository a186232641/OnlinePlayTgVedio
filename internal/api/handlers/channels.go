package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
)

type ChannelsHandlers struct {
	DB *db.DB
}

type channelDTO struct {
	ID            int64  `json:"id"`
	TGChannelID   int64  `json:"tg_channel_id"`
	Title         string `json:"title"`
	Username      string `json:"username,omitempty"`
	VideoCount    int    `json:"video_count"`
	LastIndexedAt string `json:"last_indexed_at,omitempty"`
}

func (h *ChannelsHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	chs, err := h.DB.ListChannels(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]channelDTO, 0, len(chs))
	for _, c := range chs {
		dto := channelDTO{
			ID:          c.ID,
			TGChannelID: c.TGChannelID,
			Title:       c.Title,
			Username:    c.Username,
			VideoCount:  c.VideoCount,
		}
		if c.LastIndexedAt != nil {
			dto.LastIndexedAt = c.LastIndexedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, dto)
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
