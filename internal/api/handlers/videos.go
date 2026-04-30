package handlers

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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
	if v.ThumbPath == "" {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "no_thumb", "no thumbnail available"))
		return
	}
	// Prevent traversal: thumb path is repo-controlled (set by indexer), but
	// be defensive.
	clean := filepath.Clean(v.ThumbPath)
	if strings.HasPrefix(clean, "..") || strings.ContainsRune(clean, ':') {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_path", "invalid thumb path"))
		return
	}
	abs := filepath.Join(h.Cfg.CacheDir, clean)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, abs)
}

func (h *VideosHandlers) Search(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"videos": []videoDTO{}})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	vids, err := h.DB.SearchVideos(r.Context(), uid, q, limit)
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
