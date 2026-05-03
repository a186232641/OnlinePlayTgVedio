package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
)

// Telegram Desktop's "Export chat history" JSON shape (only fields we need).
// Forward-compat: unknown fields are ignored.
type tdExport struct {
	Name     string       `json:"name"`
	Type     string       `json:"type"`
	ID       int64        `json:"id"`
	Messages []tdMessage `json:"messages"`
}

type tdMessage struct {
	ID           int             `json:"id"`
	Type         string          `json:"type"`
	Date         string          `json:"date"`
	DateUnix     string          `json:"date_unixtime"`
	MediaType    string          `json:"media_type"`
	File         string          `json:"file"`
	FileSize     json.RawMessage `json:"file_size"` // can be number or quoted string
	MimeType     string          `json:"mime_type"`
	Duration     int             `json:"duration_seconds"`
	Width        int             `json:"width"`
	Height      int             `json:"height"`
	Text         json.RawMessage `json:"text"` // string OR array of fragments
	ForumTopicID int             `json:"forum_topic_id"`
}

// parseFileSize handles both bare number ("file_size":12345) and quoted
// number ("file_size":"12345") forms — TG Desktop has used both across
// versions.
func parseFileSize(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	}
	return 0
}

// captionFromText collapses TG export's text field, which is either a plain
// string or an array of {type, text} fragments, into a single caption string.
func captionFromText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		var leaf string
		if json.Unmarshal(p, &leaf) == nil {
			b.WriteString(leaf)
			continue
		}
		var obj struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(p, &obj) == nil {
			b.WriteString(obj.Text)
		}
	}
	return b.String()
}

func (m *tdMessage) sentAt() *time.Time {
	if m.DateUnix != "" {
		if n, err := strconv.ParseInt(m.DateUnix, 10, 64); err == nil {
			t := time.Unix(n, 0).UTC()
			return &t
		}
	}
	if m.Date != "" {
		if t, err := time.Parse("2006-01-02T15:04:05", m.Date); err == nil {
			return &t
		}
	}
	return nil
}

func (m *tdMessage) isVideo() bool {
	switch m.MediaType {
	case "video_file", "video_message":
		return true
	}
	if strings.HasPrefix(strings.ToLower(m.MimeType), "video/") {
		return true
	}
	return false
}

// Import accepts a multipart upload of Telegram Desktop's result.json and
// populates videos for the target channel/topic. Locator fields (tg_doc_id,
// access_hash, file_reference) are left as 0/empty — they get filled on the
// first play via the existing refresh path.
//
// POST /api/channels/:id/import   (multipart, key=file)
func (h *ChannelsHandlers) Import(w http.ResponseWriter, r *http.Request) {
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

	// Cap upload at 1 GiB. Real-world JSON exports are typically <100 MiB.
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_upload", "上传失败: "+err.Error()))
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "no_file", "缺少 file 字段(multipart)"))
		return
	}
	defer f.Close()

	var exp tdExport
	if err := json.NewDecoder(f).Decode(&exp); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_json", "JSON 解析失败: "+err.Error()))
		return
	}

	// Sanity check: the JSON's chat id should match the channel's tg_channel_id.
	// Tolerate the -100xxx MTProto bot-API id form by stripping the prefix.
	jsonChan := normalizeChannelID(exp.ID)
	if jsonChan != 0 && jsonChan != ch.TGChannelID {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "channel_mismatch",
			fmt.Sprintf("JSON 里的频道 id (%d) 与目标频道 (%d) 不一致,请确认上传文件正确", jsonChan, ch.TGChannelID)))
		return
	}

	imported, skipped := 0, 0
	emptyRef := []byte{}
	for _, m := range exp.Messages {
		if m.Type != "message" || !m.isVideo() {
			skipped++
			continue
		}
		v := &db.Video{
			UserID:        uid,
			ChannelID:     cid,
			TGMessageID:   int64(m.ID),
			TGDocID:       0, // resolved on first play
			AccessHash:    0,
			FileReference: emptyRef,
			Mime:          m.MimeType,
			SizeBytes:     parseFileSize(m.FileSize),
			DurationSec:   m.Duration,
			Width:         m.Width,
			Height:        m.Height,
			Caption:       captionFromText(m.Text),
			SentAt:        m.sentAt(),
		}
		if _, err := h.DB.UpsertVideo(r.Context(), v); err != nil {
			httpx.WriteError(w, fmt.Errorf("upsert video msg=%d: %w", m.ID, err))
			return
		}
		imported++
	}

	if err := h.DB.MarkChannelIndexed(r.Context(), cid, imported); err != nil {
		httpx.WriteError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"imported": imported,
		"skipped":  skipped,
		"total":    len(exp.Messages),
	})
}

// normalizeChannelID strips the -100xxx Bot API prefix that some exports use
// to encode supergroup/channel ids.
func normalizeChannelID(id int64) int64 {
	if id < 0 {
		// -1001234567890 → 1234567890
		s := strconv.FormatInt(-id, 10)
		s = strings.TrimPrefix(s, "100")
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	return id
}
