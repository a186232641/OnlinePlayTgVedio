package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
)

// CollectionsHandlers serve the user's own named groups of videos/images.
// Independent of favorites: nothing here favorites or pins anything.
type CollectionsHandlers struct {
	DB *db.DB
}

const maxCollectionName = 64

type collectionDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	CreatedAt     string `json:"created_at"`
	Videos        int64  `json:"videos"`
	Photos        int64  `json:"photos"`
	LastAddedAt   string `json:"last_added_at,omitempty"`
	CoverKind     string `json:"cover_kind,omitempty"`
	CoverThumbURL string `json:"cover_thumb_url,omitempty"`
	// Contains is set only when the listing was asked about one media row
	// (?kind=&id=) — the "加入分组" picker's ticks.
	Contains *bool `json:"contains,omitempty"`
}

func collectionToDTO(c db.Collection) collectionDTO {
	d := collectionDTO{
		ID:        c.ID,
		Name:      c.Name,
		CreatedAt: c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Videos:    c.Videos,
		Photos:    c.Photos,
	}
	if c.LastAddedAt != nil {
		d.LastAddedAt = c.LastAddedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if c.CoverID != 0 {
		base := "/api/videos/"
		if c.CoverKind == db.MediaKindPhoto {
			base = "/api/photos/"
		}
		d.CoverKind = c.CoverKind
		d.CoverThumbURL = base + strconv.FormatInt(c.CoverID, 10) + "/thumb"
	}
	return d
}

// collectionName validates a create/rename body's name.
func collectionName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", httpx.Errorf(http.StatusBadRequest, "bad_name", "name is required")
	}
	if utf8.RuneCountInString(name) > maxCollectionName {
		return "", httpx.Errorf(http.StatusBadRequest, "bad_name", "name is too long")
	}
	return name, nil
}

func nameTakenOr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return httpx.Errorf(http.StatusConflict, "name_taken", "a collection with this name already exists")
	}
	return err
}

func notFoundOr(err error) error {
	if errors.Is(err, db.ErrCollectionNotFound) {
		return httpx.Errorf(http.StatusNotFound, "not_found", "collection not found")
	}
	return err
}

// collection resolves {id} to one of the user's collections.
func (h *CollectionsHandlers) collection(r *http.Request) (*db.Collection, error) {
	uid, _ := web.UserIDFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return nil, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid collection id")
	}
	c, err := h.DB.CollectionByID(r.Context(), uid, id)
	return c, notFoundOr(err)
}

// List returns the user's collections, most recently used first. With
// ?kind=video|photo&id=N each also says whether it holds that media row.
//
// GET /api/collections/
func (h *CollectionsHandlers) List(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cs, err := h.DB.ListCollections(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	qv := r.URL.Query()
	var holding map[int64]bool
	if mid, _ := strconv.ParseInt(qv.Get("id"), 10, 64); mid != 0 {
		kind := normalizeKind(qv.Get("kind"))
		if kind == "" {
			kind = db.MediaKindVideo
		}
		if holding, err = h.DB.CollectionsContaining(r.Context(), uid, kind, mid); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	out := make([]collectionDTO, 0, len(cs))
	for _, c := range cs {
		d := collectionToDTO(c)
		if holding != nil {
			has := holding[c.ID]
			d.Contains = &has
		}
		out = append(out, d)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// Get returns one collection's summary.
//
// GET /api/collections/:id
func (h *CollectionsHandlers) Get(w http.ResponseWriter, r *http.Request) {
	c, err := h.collection(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, collectionToDTO(*c))
}

// Create makes a new, empty collection.
//
// POST /api/collections/   body: {"name": "最爱"}
func (h *CollectionsHandlers) Create(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	name, err := collectionName(req.Name)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	c, err := h.DB.CreateCollection(r.Context(), uid, name)
	if err != nil {
		httpx.WriteError(w, nameTakenOr(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, collectionToDTO(*c))
}

// Rename changes a collection's name.
//
// PATCH /api/collections/:id   body: {"name": "..."}
func (h *CollectionsHandlers) Rename(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid collection id"))
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	name, err := collectionName(req.Name)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.DB.RenameCollection(r.Context(), uid, id, name); err != nil {
		httpx.WriteError(w, notFoundOr(nameTakenOr(err)))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Delete removes a collection; its videos and images are untouched.
//
// DELETE /api/collections/:id
func (h *CollectionsHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid collection id"))
		return
	}
	if err := h.DB.DeleteCollection(r.Context(), uid, id); err != nil {
		httpx.WriteError(w, notFoundOr(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Media is the collection's merged video+image list — the favorites listing's
// shape, filters and orders, with fav_* meaning the time added to the group
// (the default).
//
// GET /api/collections/:id/media?kind=&file_name=&date_from=&date_to=&order=&limit=&offset_video=&offset_photo=
func (h *CollectionsHandlers) Media(w http.ResponseWriter, r *http.Request) {
	c, err := h.collection(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	uid, _ := web.UserIDFromContext(r.Context())
	qv := r.URL.Query()
	limit, _ := strconv.Atoi(qv.Get("limit"))
	items, next, hasMore, err := h.DB.ListMedia(r.Context(), db.ListMediaOpts{
		UserID:       uid,
		CollectionID: c.ID,
		Kind:         normalizeKind(qv.Get("kind")),
		Q:            strings.TrimSpace(qv.Get("q")),
		FileName:     strings.TrimSpace(qv.Get("file_name")),
		DateFrom:     parseDateOnly(qv.Get("date_from"), false),
		DateTo:       parseDateOnly(qv.Get("date_to"), true),
		OrderBy:      qv.Get("order"),
		Limit:        limit,
		Cursor:       mediaCursorFromQuery(r),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// A collection mixes channels and topics, so each page names its sources.
	sources, err := mediaSources(r, h.DB, uid, items)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	writeMediaPage(w, items, next, hasMore, map[string]any{"sources": sources})
}

// memberReq names one media row: {"kind": "video"|"photo", "id": N}.
type memberReq struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id"`
}

// AddItem puts a video or image into the collection.
//
// POST /api/collections/:id/items   body: {"kind": "photo", "id": 12}
func (h *CollectionsHandlers) AddItem(w http.ResponseWriter, r *http.Request) {
	c, err := h.collection(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	uid, _ := web.UserIDFromContext(r.Context())
	var req memberReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	kind := normalizeKind(req.Kind)
	if kind == "" || req.ID == 0 {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_item", "kind (video|photo) and id are required"))
		return
	}
	// The media row must be the user's too.
	if kind == db.MediaKindPhoto {
		_, err = h.DB.PhotoByID(r.Context(), req.ID, uid)
	} else {
		_, err = h.DB.VideoByID(r.Context(), req.ID, uid)
	}
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", kind+" not found"))
		return
	}
	if err := h.DB.AddToCollection(r.Context(), c.ID, kind, req.ID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// RemoveItem takes a video or image out of the collection.
//
// DELETE /api/collections/:id/items/:kind/:item_id
func (h *CollectionsHandlers) RemoveItem(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid collection id"))
		return
	}
	kind := normalizeKind(chi.URLParam(r, "kind"))
	mid, err := strconv.ParseInt(chi.URLParam(r, "item_id"), 10, 64)
	if kind == "" || err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_item", "invalid kind or id"))
		return
	}
	if err := h.DB.RemoveFromCollection(r.Context(), uid, cid, kind, mid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
