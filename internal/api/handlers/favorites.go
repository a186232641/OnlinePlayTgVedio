package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
)

type FavoritesHandlers struct {
	DB       *db.DB
	OnAdd    func(userID, videoID int64) // hook into cache worker (pin + download)
	OnRemove func(userID, videoID int64)
	// Photo equivalents — images are a separate table with a separate favorites
	// table, so they need their own pin/unpin hooks.
	OnPhotoAdd    func(userID, photoID int64)
	OnPhotoRemove func(userID, photoID int64)
}

// favReq accepts both the original video-only shape ({"video_id": 1}) and the
// kind-tagged shape ({"kind": "photo", "id": 1}) so older clients keep working.
type favReq struct {
	VideoID int64  `json:"video_id"`
	PhotoID int64  `json:"photo_id"`
	Kind    string `json:"kind"`
	ID      int64  `json:"id"`
}

// target resolves the request to (kind, id).
func (f favReq) target() (string, int64) {
	switch {
	case f.PhotoID != 0:
		return db.MediaKindPhoto, f.PhotoID
	case f.VideoID != 0:
		return db.MediaKindVideo, f.VideoID
	case f.Kind == db.MediaKindPhoto:
		return db.MediaKindPhoto, f.ID
	default:
		return db.MediaKindVideo, f.ID
	}
}

// List returns the user's favorites — videos and images merged, newest first.
// It accepts the same file_name / date_from / date_to / order / kind filters as
// the media search so the favorites page can search within favorites.
func (h *FavoritesHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	qv := r.URL.Query()
	limit, _ := strconv.Atoi(qv.Get("limit"))

	items, next, hasMore, err := h.DB.ListMedia(r.Context(), db.ListMediaOpts{
		UserID:   uid,
		FavOnly:  true,
		Kind:     normalizeKind(qv.Get("kind")),
		Q:        strings.TrimSpace(qv.Get("q")),
		FileName: strings.TrimSpace(qv.Get("file_name")),
		DateFrom: parseDateOnly(qv.Get("date_from"), false),
		DateTo:   parseDateOnly(qv.Get("date_to"), true),
		OrderBy:  qv.Get("order"),
		Limit:    limit,
		Cursor:   mediaCursorFromQuery(r),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	writeMediaPage(w, items, next, hasMore, nil)
}

func (h *FavoritesHandlers) Add(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	var req favReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	kind, id := req.target()
	if id == 0 {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "missing media id"))
		return
	}
	if kind == db.MediaKindPhoto {
		if _, err := h.DB.PhotoByID(r.Context(), id, uid); err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "photo not found"))
			return
		}
		if err := h.DB.AddPhotoFavorite(r.Context(), uid, id); err != nil {
			httpx.WriteError(w, err)
			return
		}
		if h.OnPhotoAdd != nil {
			h.OnPhotoAdd(uid, id)
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if _, err := h.DB.VideoByID(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "video not found"))
		return
	}
	if err := h.DB.AddFavorite(r.Context(), uid, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if h.OnAdd != nil {
		h.OnAdd(uid, id)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Remove drops a video favorite: DELETE /api/favorites/:video_id
func (h *FavoritesHandlers) Remove(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "video_id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid video id"))
		return
	}
	if err := h.DB.RemoveFavorite(r.Context(), uid, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if h.OnRemove != nil {
		h.OnRemove(uid, id)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RemovePhoto drops an image favorite: DELETE /api/favorites/photo/:id
func (h *FavoritesHandlers) RemovePhoto(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid photo id"))
		return
	}
	if err := h.DB.RemovePhotoFavorite(r.Context(), uid, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if h.OnPhotoRemove != nil {
		h.OnPhotoRemove(uid, id)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
