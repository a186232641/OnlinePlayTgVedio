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
	DB        *db.DB
	OnAdd     func(userID, videoID int64) // hook into cache worker (Phase 7)
	OnRemove  func(userID, videoID int64)
}

type favReq struct {
	VideoID int64 `json:"video_id"`
}

// List returns the user's favorites. It also accepts the same file_name /
// date_from / date_to / order filters as /videos/search (any subset, all
// optional) so the favorites page can search and sort within favorites — these
// are threaded through SearchVideos with FavOnly so they share one query path.
func (h *FavoritesHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	qv := r.URL.Query()
	limit, _ := strconv.Atoi(qv.Get("limit"))
	offsetID, _ := strconv.ParseInt(qv.Get("offset_id"), 10, 64)
	vids, err := h.DB.SearchVideos(r.Context(), db.SearchVideosOpts{
		UserID:   uid,
		FavOnly:  true,
		FileName: strings.TrimSpace(qv.Get("file_name")),
		DateFrom: parseDateOnly(qv.Get("date_from"), false),
		DateTo:   parseDateOnly(qv.Get("date_to"), true),
		OrderBy:  qv.Get("order"),
		Limit:    limit,
		OffsetID: offsetID,
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

func (h *FavoritesHandlers) Add(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	var req favReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if _, err := h.DB.VideoByID(r.Context(), req.VideoID, uid); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "video not found"))
		return
	}
	if err := h.DB.AddFavorite(r.Context(), uid, req.VideoID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if h.OnAdd != nil {
		h.OnAdd(uid, req.VideoID)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

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
