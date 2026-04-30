package handlers

import (
	"net/http"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/indexer"
)

type IndexHandlers struct {
	DB      *db.DB
	Indexer *indexer.Indexer
}

func (h *IndexHandlers) Status(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	job, err := h.DB.GetIndexJob(r.Context(), uid)
	if err == db.ErrNotFound {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "idle"})
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":         job.Status,
		"channels_total": job.ChannelsTotal,
		"channels_done":  job.ChannelsDone,
		"videos_found":   job.VideosFound,
		"last_error":     job.LastError,
	})
}

func (h *IndexHandlers) Start(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	h.Indexer.Trigger(uid)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
