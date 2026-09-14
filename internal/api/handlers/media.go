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

// mediaDTO is the one shape the frontend renders in every grid: a video and an
// image differ only by `kind` and which fields are meaningful. Field names stay
// aligned with videoDTO (and thus with TG Desktop's export) so the two can be
// mixed in one list without a translation layer.
type mediaDTO struct {
	Kind            string `json:"kind"` // "video" | "photo"
	ID              int64  `json:"id"`
	ChannelID       int64  `json:"channel_id"`
	TGMsgID         int64  `json:"tg_msg_id"`
	Date            string `json:"date,omitempty"`
	FromName        string `json:"from,omitempty"`
	FileName        string `json:"file_name,omitempty"`
	FileSize        int64  `json:"file_size"`
	MediaType       string `json:"media_type,omitempty"`
	MimeType        string `json:"mime_type,omitempty"`
	DurationSeconds int    `json:"duration_seconds"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	Text            string `json:"text"`
	// URL plays the video / loads the full image; ThumbURL is the grid tile.
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url"`
}

func videoToMediaDTO(v db.Video) mediaDTO {
	id := strconv.FormatInt(v.ID, 10)
	d := mediaDTO{
		Kind:            db.MediaKindVideo,
		ID:              v.ID,
		ChannelID:       v.ChannelID,
		TGMsgID:         v.TGMsgID,
		FromName:        v.FromName,
		FileName:        v.FileName,
		FileSize:        v.FileSize,
		MediaType:       v.MediaType,
		MimeType:        v.MimeType,
		DurationSeconds: v.DurationSeconds,
		Width:           v.Width,
		Height:          v.Height,
		Text:            v.Text,
		URL:             "/api/videos/" + id + "/stream",
		ThumbURL:        "/api/videos/" + id + "/thumb",
	}
	if v.Date != nil {
		d.Date = v.Date.Format("2006-01-02T15:04:05Z07:00")
	}
	return d
}

func photoToMediaDTO(p db.Photo) mediaDTO {
	id := strconv.FormatInt(p.ID, 10)
	d := mediaDTO{
		Kind:      db.MediaKindPhoto,
		ID:        p.ID,
		ChannelID: p.ChannelID,
		TGMsgID:   p.TGMsgID,
		FromName:  p.FromName,
		FileName:  p.FileName,
		FileSize:  p.FileSize,
		MediaType: "photo",
		MimeType:  "image/jpeg",
		Width:     p.Width,
		Height:    p.Height,
		Text:      p.Text,
		URL:       "/api/photos/" + id + "/file",
		ThumbURL:  "/api/photos/" + id + "/thumb",
	}
	if p.Date != nil {
		d.Date = p.Date.Format("2006-01-02T15:04:05Z07:00")
	}
	return d
}

func mediaToDTO(m db.MediaItem) mediaDTO {
	if m.Kind == db.MediaKindPhoto {
		return photoToMediaDTO(*m.Photo)
	}
	return videoToMediaDTO(*m.Video)
}

// mediaCursorFromQuery reads the two-part keyset cursor. Videos and photos are
// separate tables with separate id sequences, so a merged list carries one
// cursor per kind (see db.MediaCursor).
func mediaCursorFromQuery(r *http.Request) db.MediaCursor {
	q := r.URL.Query()
	v, _ := strconv.ParseInt(q.Get("offset_video"), 10, 64)
	p, _ := strconv.ParseInt(q.Get("offset_photo"), 10, 64)
	return db.MediaCursor{VideoID: v, PhotoID: p}
}

// normalizeKind maps the ?kind= filter onto a db constant. Anything unknown
// (including "all"/"") means both kinds.
func normalizeKind(s string) string {
	switch strings.TrimSpace(s) {
	case db.MediaKindVideo:
		return db.MediaKindVideo
	case db.MediaKindPhoto:
		return db.MediaKindPhoto
	default:
		return ""
	}
}

func writeMediaPage(w http.ResponseWriter, items []db.MediaItem, next db.MediaCursor, hasMore bool, extra map[string]any) {
	out := make([]mediaDTO, 0, len(items))
	for _, it := range items {
		out = append(out, mediaToDTO(it))
	}
	resp := map[string]any{
		"items":    out,
		"next":     next,
		"has_more": hasMore,
	}
	for k, v := range extra {
		resp[k] = v
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// ChannelMedia is the merged video+image listing of one channel or topic.
//
// GET /api/channels/:id/media?kind=&order=&limit=&offset_video=&offset_photo=&q=
func (h *ChannelsHandlers) ChannelMedia(w http.ResponseWriter, r *http.Request) {
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
	qv := r.URL.Query()
	limit, _ := strconv.Atoi(qv.Get("limit"))
	cursor := mediaCursorFromQuery(r)

	items, next, hasMore, err := h.DB.ListMedia(r.Context(), db.ListMediaOpts{
		UserID:    uid,
		ChannelID: cid,
		Kind:      normalizeKind(qv.Get("kind")),
		Q:         strings.TrimSpace(qv.Get("q")),
		OrderBy:   qv.Get("order"),
		Limit:     limit,
		Cursor:    cursor,
		// streamer present (even empty, = the NULL bucket) ⇒ filter on it.
		StreamerFilter: qv.Has("streamer"),
		Streamer:       qv.Get("streamer"),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	extra := map[string]any{}
	// Totals come from the counters on the channel row, which MarkChannelIndexed
	// recomputes after every sync/import/clear. Counting the rows here instead
	// would mean two full COUNT(*)s per first page — on a million-row channel
	// that is hundreds of milliseconds on every open, to render a number that
	// only changes when a sync finishes. The trade is that the figure is stale
	// while a sync is mid-flight, which the progress line already covers.
	// Skipped under a streamer filter: that count comes from /streamers.
	if cursor.VideoID == 0 && cursor.PhotoID == 0 && !qv.Has("streamer") {
		extra["total_videos"] = ch.VideoCount
		extra["total_photos"] = ch.PhotoCount
	}
	writeMediaPage(w, items, next, hasMore, extra)
}

// MediaSearch is /videos/search widened to images.
//
// GET /api/media/search?q=&text=&file_name=&date_from=&date_to=&channel_id=&kind=
func (h *ChannelsHandlers) MediaSearch(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	text := strings.TrimSpace(qv.Get("text"))
	fileName := strings.TrimSpace(qv.Get("file_name"))
	dateFrom := parseDateOnly(qv.Get("date_from"), false)
	dateTo := parseDateOnly(qv.Get("date_to"), true)

	// Same guard as /videos/search: an empty query must not dump the library.
	if q == "" && text == "" && fileName == "" && dateFrom == nil && dateTo == nil {
		writeMediaPage(w, nil, db.MediaCursor{}, false, nil)
		return
	}

	limit, _ := strconv.Atoi(qv.Get("limit"))
	channelID, _ := strconv.ParseInt(qv.Get("channel_id"), 10, 64)

	items, next, hasMore, err := h.DB.ListMedia(r.Context(), db.ListMediaOpts{
		UserID:    uid,
		ChannelID: channelID,
		Kind:      normalizeKind(qv.Get("kind")),
		Q:         q,
		Text:      text,
		FileName:  fileName,
		DateFrom:  dateFrom,
		DateTo:    dateTo,
		OrderBy:   qv.Get("order"),
		Limit:     limit,
		Cursor:    mediaCursorFromQuery(r),
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	// Results span channels and topics, same as favorites — label each origin.
	sources, err := mediaSources(r, h.DB, uid, items)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	writeMediaPage(w, items, next, hasMore, map[string]any{"sources": sources})
}

// sourceDTO names where a media item came from. For a topic it also carries the
// forum group, so the UI can link to either level.
type sourceDTO struct {
	ID              int64  `json:"id"`
	Title           string `json:"title"`
	DialogKind      string `json:"dialog_kind"`
	ParentChannelID int64  `json:"parent_channel_id,omitempty"`
	ParentTitle     string `json:"parent_title,omitempty"`
}

// mediaSources resolves the distinct channels of a page, keyed by channel id.
// It is a side map rather than fields on every item: a page of 120 favorites
// usually comes from a handful of channels, so repeating titles per item would
// only bloat the payload.
func mediaSources(r *http.Request, database *db.DB, uid int64, items []db.MediaItem) (map[int64]sourceDTO, error) {
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, 8)
	for _, it := range items {
		cid := it.ChannelID()
		if _, ok := seen[cid]; ok {
			continue
		}
		seen[cid] = struct{}{}
		ids = append(ids, cid)
	}
	srcs, err := database.ChannelSources(r.Context(), uid, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]sourceDTO, len(srcs))
	for id, s := range srcs {
		out[id] = sourceDTO{
			ID: s.ID, Title: s.Title, DialogKind: s.DialogKind,
			ParentChannelID: s.ParentID, ParentTitle: s.ParentTitle,
		}
	}
	return out, nil
}
