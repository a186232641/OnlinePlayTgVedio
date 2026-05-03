package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/indexer"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tglogin"
)

type TGLoginHandlers struct {
	DB      *db.DB
	Login   *tglogin.Manager
	TGMgr   *tgmanager.Manager
	Indexer *indexer.Indexer
}

type tgStartReq struct {
	Phone string `json:"phone"`
}

type tgFlowResp struct {
	FlowID string        `json:"flow_id"`
	Stage  tglogin.Stage `json:"stage"`
}

type tgCodeReq struct {
	FlowID string `json:"flow_id"`
	Code   string `json:"code"`
}

type tgPasswordReq struct {
	FlowID   string `json:"flow_id"`
	Password string `json:"password"`
}

type tgSessionDTO struct {
	ID             int64  `json:"id"`
	Phone          string `json:"phone,omitempty"`
	TGUserID       int64  `json:"tg_user_id,omitempty"`
	Label          string `json:"label,omitempty"`
	Status         string `json:"status"`
	DiscoverStatus string `json:"discover_status,omitempty"`
	DiscoverError  string `json:"discover_error,omitempty"`
}

type sessionLabelReq struct {
	Label string `json:"label"`
}

func sessionToDTO(s db.TGSession) tgSessionDTO {
	return tgSessionDTO{
		ID:             s.ID,
		Phone:          s.Phone,
		TGUserID:       s.TGUserID,
		Label:          s.Label,
		Status:         string(s.Status),
		DiscoverStatus: s.DiscoverStatus,
		DiscoverError:  s.DiscoverError,
	}
}

func (h *TGLoginHandlers) Start(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	var req tgStartReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	id, stage, err := h.Login.Start(r.Context(), uid, req.Phone)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "tg_start_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, tgFlowResp{FlowID: id, Stage: stage})
}

func (h *TGLoginHandlers) Code(w http.ResponseWriter, r *http.Request) {
	var req tgCodeReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	stage, err := h.Login.SubmitCode(r.Context(), req.FlowID, req.Code)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "tg_code_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, tgFlowResp{FlowID: req.FlowID, Stage: stage})
}

func (h *TGLoginHandlers) Password(w http.ResponseWriter, r *http.Request) {
	var req tgPasswordReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	stage, err := h.Login.SubmitPassword(r.Context(), req.FlowID, req.Password)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "tg_password_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, tgFlowResp{FlowID: req.FlowID, Stage: stage})
}

// ListSessions returns every TG account the current user has bound (excluding revoked).
func (h *TGLoginHandlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	sessions, err := h.DB.ListTGSessions(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	out := make([]tgSessionDTO, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionToDTO(s))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (h *TGLoginHandlers) RefreshDiscover(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	sid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid session id"))
		return
	}
	if _, err := h.DB.GetTGSessionByID(r.Context(), sid, uid); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "session not found"))
		return
	}
	if h.Indexer != nil {
		h.Indexer.TriggerDiscover(sid)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *TGLoginHandlers) DeleteSession(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	sid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid session id"))
		return
	}
	if _, err := h.DB.GetTGSessionByID(r.Context(), sid, uid); err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "session not found"))
		return
	}
	if h.TGMgr != nil {
		_ = h.TGMgr.Stop(sid)
	}
	if err := h.DB.RevokeTGSession(r.Context(), sid, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *TGLoginHandlers) UpdateLabel(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	sid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid session id"))
		return
	}
	var req sessionLabelReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.DB.UpdateTGSessionLabel(r.Context(), sid, uid, req.Label); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
