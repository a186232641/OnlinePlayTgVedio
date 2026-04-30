package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func Errorf(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, err error) {
	var herr *Error
	if errors.As(err, &herr) {
		WriteJSON(w, herr.Status, herr)
		return
	}
	slog.Error("internal error", "err", err)
	WriteJSON(w, http.StatusInternalServerError, &Error{Code: "internal", Message: "internal server error"})
}

func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return Errorf(http.StatusBadRequest, "bad_request", "invalid JSON body")
	}
	return nil
}
