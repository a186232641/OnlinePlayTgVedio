package handlers

import (
	"net/http"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tglogin"
)

type TGLoginHandlers struct {
	DB    *db.DB
	Login *tglogin.Manager
}

type tgStartReq struct {
	Phone string `json:"phone"`
}

type tgFlowResp struct {
	FlowID string         `json:"flow_id"`
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

type tgStatusResp struct {
	Bound    bool   `json:"bound"`
	Phone    string `json:"phone,omitempty"`
	TGUserID int64  `json:"tg_user_id,omitempty"`
	Status   string `json:"status,omitempty"`
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

func (h *TGLoginHandlers) Status(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	s, err := h.DB.GetTGSession(r.Context(), uid)
	if err == db.ErrNotFound {
		httpx.WriteJSON(w, http.StatusOK, tgStatusResp{Bound: false})
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, tgStatusResp{
		Bound:    s.Status == db.TGStatusActive,
		Phone:    s.Phone,
		TGUserID: s.TGUserID,
		Status:   string(s.Status),
	})
}
