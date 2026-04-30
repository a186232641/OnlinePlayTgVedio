package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
)

type AuthHandlers struct {
	Cfg *config.Config
	DB  *db.DB
}

type registerReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResp struct {
	UserID int64  `json:"user_id"`
	Email  string `json:"email"`
}

func (h *AuthHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if !validEmail(req.Email) {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_email", "invalid email"))
		return
	}
	if len(req.Password) < 8 {
		httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "weak_password", "password must be at least 8 characters"))
		return
	}
	hash, err := web.HashPassword(req.Password)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	u, err := h.DB.CreateUser(r.Context(), req.Email, hash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			httpx.WriteError(w, httpx.Errorf(http.StatusConflict, "email_taken", "email already registered"))
			return
		}
		httpx.WriteError(w, err)
		return
	}
	h.issueAndSet(w, r, u)
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	u, err := h.DB.UserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpx.WriteError(w, httpx.Errorf(http.StatusUnauthorized, "invalid_credentials", "invalid email or password"))
			return
		}
		httpx.WriteError(w, err)
		return
	}
	ok, err := web.VerifyPassword(req.Password, u.PasswordHash)
	if err != nil || !ok {
		httpx.WriteError(w, httpx.Errorf(http.StatusUnauthorized, "invalid_credentials", "invalid email or password"))
		return
	}
	h.issueAndSet(w, r, u)
}

func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	web.ClearSessionCookie(w, secureCookie(r))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	uid, _ := web.UserIDFromContext(r.Context())
	u, err := h.DB.UserByID(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, authResp{UserID: u.ID, Email: u.Email})
}

func (h *AuthHandlers) issueAndSet(w http.ResponseWriter, r *http.Request, u *db.User) {
	tok, _, err := web.IssueToken(h.Cfg.JWTSecret, u.ID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	web.SetSessionCookie(w, tok, secureCookie(r))
	httpx.WriteJSON(w, http.StatusOK, authResp{UserID: u.ID, Email: u.Email})
}

func secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func validEmail(s string) bool {
	if len(s) < 3 || len(s) > 254 {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}
