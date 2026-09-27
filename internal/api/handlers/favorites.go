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
// the media search so the favorites page can search within favorites, plus
// channel_id / streamer to open one card of a grouped view (streamer present,
// even empty = the no-streamer bucket, filters on it — as in ChannelMedia).
func (h *FavoritesHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	qv := r.URL.Query()
	limit, _ := strconv.Atoi(qv.Get("limit"))
	channelID, _ := strconv.ParseInt(qv.Get("channel_id"), 10, 64)

	items, next, hasMore, err := h.DB.ListMedia(r.Context(), db.ListMediaOpts{
		UserID:    uid,
		ChannelID: channelID,
		FavOnly:   true,
		Kind:      normalizeKind(qv.Get("kind")),
		Q:         strings.TrimSpace(qv.Get("q")),
		FileName:  strings.TrimSpace(qv.Get("file_name")),
		DateFrom:  parseDateOnly(qv.Get("date_from"), false),
		DateTo:    parseDateOnly(qv.Get("date_to"), true),
		OrderBy:   qv.Get("order"),
		Limit:     limit,
		Cursor:    mediaCursorFromQuery(r),

		StreamerFilter: qv.Has("streamer"),
		Streamer:       qv.Get("streamer"),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// Favorites span every channel and topic, so each page carries where its
	// items came from — the grid links back to the source.
	sources, err := mediaSources(r, h.DB, uid, items)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	writeMediaPage(w, items, next, hasMore, map[string]any{"sources": sources})
}

// favGroupDTO is one card of a grouped favorites view. `by` tags what the
// group is, so the UI knows how to title it and which filter opens it:
// source → ?channel_id=<key>, streamer → ?streamer=<key>.
type favGroupDTO struct {
	By              string     `json:"by"`
	Key             string     `json:"key"`
	Source          *sourceDTO `json:"source,omitempty"` // by=source only
	Videos          int64      `json:"videos"`
	Photos          int64      `json:"photos"`
	LastFavoritedAt string     `json:"last_favorited_at"`
	CoverKind       string     `json:"cover_kind"`
	CoverThumbURL   string     `json:"cover_thumb_url"`
}

// Groups buckets the user's favorites so a topic (or streamer) with many
// favorited items shows up once rather than item by item.
//
// GET /api/favorites/groups?by=source|streamer&kind=
func (h *FavoritesHandlers) Groups(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	qv := r.URL.Query()
	by := qv.Get("by")
	if by != db.FavGroupBySource && by != db.FavGroupByStreamer {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_group", "by must be source or streamer"))
		return
	}
	groups, err := h.DB.FavoriteGroups(r.Context(), uid, by, normalizeKind(qv.Get("kind")))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var srcs map[int64]db.ChannelSource
	if by == db.FavGroupBySource {
		ids := make([]int64, 0, len(groups))
		for _, g := range groups {
			id, _ := strconv.ParseInt(g.Key, 10, 64)
			ids = append(ids, id)
		}
		if srcs, err = h.DB.ChannelSources(r.Context(), uid, ids); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	out := make([]favGroupDTO, 0, len(groups))
	for _, g := range groups {
		thumb := "/api/videos/"
		if g.CoverKind == db.MediaKindPhoto {
			thumb = "/api/photos/"
		}
		d := favGroupDTO{
			By:              by,
			Key:             g.Key,
			Videos:          g.Videos,
			Photos:          g.Photos,
			LastFavoritedAt: g.LastFavoritedAt.Format("2006-01-02T15:04:05Z07:00"),
			CoverKind:       g.CoverKind,
			CoverThumbURL:   thumb + strconv.FormatInt(g.CoverID, 10) + "/thumb",
		}
		if by == db.FavGroupBySource {
			id, _ := strconv.ParseInt(g.Key, 10, 64)
			s, ok := srcs[id]
			if !ok {
				continue // channel row gone or not the user's
			}
			d.Source = &sourceDTO{
				ID: s.ID, Title: s.Title, DialogKind: s.DialogKind,
				ParentChannelID: s.ParentID, ParentTitle: s.ParentTitle,
			}
		}
		out = append(out, d)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
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
