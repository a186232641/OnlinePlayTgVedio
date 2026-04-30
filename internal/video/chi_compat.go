package video

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// URLParamFromRequest reads a chi-style URL param from the request.
func URLParamFromRequest(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
