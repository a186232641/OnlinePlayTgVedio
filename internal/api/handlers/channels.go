package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/indexer"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

type ChannelsHandlers struct {
	DB      *db.DB
	Indexer *indexer.Indexer
	TGMgr   *tgmanager.Manager
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

// LiveFetch pulls the next page of video messages directly from Telegram —
// for topics it uses messages.getReplies; for plain channels GetHistory. Each
// hit is upserted into videos so subsequent visits are served from DB and the
// existing /api/videos/:id/stream path works.
//
// GET /api/channels/:id/live-videos?offset_msg_id=&limit=
func (h *ChannelsHandlers) LiveFetch(w http.ResponseWriter, r *http.Request) {
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
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "forum_root", "论坛容器没有自己的消息流,请进入具体话题"))
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offsetMsg, _ := strconv.Atoi(r.URL.Query().Get("offset_msg_id"))

	cli, err := h.TGMgr.ClientForSession(ch.TGSessionID)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "tg_unavailable", "telegram client not ready"))
		return
	}

	peer, err := inputPeerForChannel(ch)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusInternalServerError, "peer_build", err.Error()))
		return
	}

	// Walk a single page; ForEach handles slicing internally so we cap by count.
	out := make([]videoDTO, 0, limit)
	collected := 0

	process := func(m *tg.Message) error {
		doc := videoDocFromMessage(m)
		if doc == nil {
			return nil
		}
		w, hh, dur := videoMetadata(doc)
		sentAt := time.Unix(int64(m.Date), 0).UTC()
		v := &db.Video{
			UserID:        uid,
			ChannelID:     cid,
			TGMessageID:   int64(m.ID),
			TGDocID:       doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
			Mime:          doc.MimeType,
			SizeBytes:     doc.Size,
			DurationSec:   dur,
			Width:         w,
			Height:        hh,
			Caption:       m.Message,
			SentAt:        &sentAt,
		}
		vid, err := h.DB.UpsertVideo(r.Context(), v)
		if err != nil {
			return err
		}
		v.ID = vid
		out = append(out, videoToDTO(*v))
		collected++
		return nil
	}

	var qErr error
	if ch.DialogKind == db.DialogKindTopic && ch.TopicID != nil {
		qb := query.Messages(cli.API).GetReplies(peer).MsgID(int(*ch.TopicID)).BatchSize(limit)
		if offsetMsg > 0 {
			qb = qb.OffsetID(offsetMsg)
		}
		qErr = qb.ForEach(r.Context(), func(_ context.Context, e messages.Elem) error {
			if collected >= limit {
				return errStopIter
			}
			if msg, ok := e.Msg.(*tg.Message); ok {
				if err := process(msg); err != nil {
					return err
				}
			}
			return nil
		})
	} else {
		qb := query.Messages(cli.API).GetHistory(peer).BatchSize(limit)
		if offsetMsg > 0 {
			qb = qb.OffsetID(offsetMsg)
		}
		qErr = qb.ForEach(r.Context(), func(_ context.Context, e messages.Elem) error {
			if collected >= limit {
				return errStopIter
			}
			if msg, ok := e.Msg.(*tg.Message); ok {
				if err := process(msg); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if qErr != nil && qErr != errStopIter {
		// Common case: not a member of channel. Surface message verbatim.
		httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_fetch", qErr.Error()))
		return
	}

	// MarkChannelIndexed updates last_indexed_at; useful even for live mode so
	// the channel surfaces with a "recent" hint in browse.
	_ = h.DB.MarkChannelIndexed(r.Context(), cid, ch.VideoCount+collected)

	resp := map[string]any{"videos": out, "fetched": collected}
	if len(out) > 0 {
		resp["next_offset_msg_id"] = out[len(out)-1].ID // not the next page anchor; client should send the smallest tg_message_id
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// inputPeerForChannel mirrors indexer.inputPeerForChannel but kept private
// here to avoid a circular import.
func inputPeerForChannel(c *db.Channel) (tg.InputPeerClass, error) {
	switch c.DialogKind {
	case db.DialogKindGroup:
		return &tg.InputPeerChat{ChatID: c.TGChannelID}, nil
	case db.DialogKindUser:
		return &tg.InputPeerUser{UserID: c.TGChannelID, AccessHash: c.AccessHash}, nil
	default:
		return &tg.InputPeerChannel{ChannelID: c.TGChannelID, AccessHash: c.AccessHash}, nil
	}
}

func videoDocFromMessage(msg *tg.Message) *tg.Document {
	media, ok := msg.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil
	}
	doc, ok := media.Document.AsNotEmpty()
	if !ok {
		return nil
	}
	for _, attr := range doc.Attributes {
		if _, ok := attr.(*tg.DocumentAttributeVideo); ok {
			return doc
		}
	}
	if strings.HasPrefix(strings.ToLower(doc.MimeType), "video/") {
		return doc
	}
	return nil
}

func videoMetadata(doc *tg.Document) (width, height, durationSec int) {
	for _, attr := range doc.Attributes {
		if a, ok := attr.(*tg.DocumentAttributeVideo); ok {
			return a.W, a.H, int(a.Duration)
		}
	}
	return 0, 0, 0
}

// errStopIter aborts an indexer ForEach early once we've collected enough.
var errStopIter = errStop("stop")

type errStop string

func (e errStop) Error() string { return string(e) }

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
	resp := map[string]any{"ok": true, "enabled": enabled}
	// Forum container: cascade toggle to every topic under it. The forum row
	// itself has no feed, so its own index_enabled stays false.
	if ch.DialogKind == db.DialogKindForum {
		n, err := h.DB.SetForumChildrenIndexEnabled(r.Context(), cid, uid, enabled)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		resp["topics_affected"] = n
	} else {
		if err := h.DB.SetChannelIndexEnabled(r.Context(), cid, uid, enabled); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	if enabled && h.Indexer != nil {
		h.Indexer.NudgeWorker(ch.TGSessionID)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
