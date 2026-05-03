package handlers

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
)

type VideosHandlers struct {
	Cfg *config.Config
	DB  *db.DB
}

func (h *VideosHandlers) Get(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid video id"))
		return
	}
	v, err := h.DB.VideoByID(r.Context(), id, uid)
	if err != nil {
		if err == db.ErrNotFound {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "video not found"))
			return
		}
		httpx.WriteError(w, err)
		return
	}
	fav, _ := h.DB.IsFavorite(r.Context(), uid, id)
	dto := videoToDTO(*v)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"video":    dto,
		"favorite": fav,
	})
}

func (h *VideosHandlers) Thumb(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid video id"))
		return
	}
	v, err := h.DB.VideoByID(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "video not found"))
		return
	}
	if v.Thumbnail == "" {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "no_thumb", "no thumbnail available"))
		return
	}
	// Prevent traversal: thumbnail path is repo-controlled (set by indexer), but
	// be defensive.
	clean := filepath.Clean(v.Thumbnail)
	if strings.HasPrefix(clean, "..") || strings.ContainsRune(clean, ':') {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_path", "invalid thumb path"))
		return
	}
	abs := filepath.Join(h.Cfg.CacheDir, clean)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, abs)
}

// Search supports any subset of these query params:
//
//	q            ILIKE %q% on (file_name OR text) — single-box search
//	text         ILIKE %text% on `text` column   (advanced AND)
//	file_name    ILIKE %file_name% on `file_name` column (advanced AND)
//	date_from    YYYY-MM-DD,inclusive lower bound
//	date_to      YYYY-MM-DD,inclusive upper bound (treated as end-of-day)
//	channel_id   restrict to one channel
//	limit, offset_id   keyset pagination (newest first)
func (h *VideosHandlers) Search(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	text := strings.TrimSpace(qv.Get("text"))
	fileName := strings.TrimSpace(qv.Get("file_name"))
	dateFrom := parseDateOnly(qv.Get("date_from"), false)
	dateTo := parseDateOnly(qv.Get("date_to"), true)

	if q == "" && text == "" && fileName == "" && dateFrom == nil && dateTo == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"videos": []videoDTO{}})
		return
	}

	limit, _ := strconv.Atoi(qv.Get("limit"))
	offsetID, _ := strconv.ParseInt(qv.Get("offset_id"), 10, 64)
	channelID, _ := strconv.ParseInt(qv.Get("channel_id"), 10, 64)

	vids, err := h.DB.SearchVideos(r.Context(), db.SearchVideosOpts{
		UserID:    uid,
		Q:         q,
		Text:      text,
		FileName:  fileName,
		DateFrom:  dateFrom,
		DateTo:    dateTo,
		ChannelID: channelID,
		Limit:     limit,
		OffsetID:  offsetID,
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

func parseDateOnly(s string, endOfDay bool) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Second)
	}
	return &t
}
