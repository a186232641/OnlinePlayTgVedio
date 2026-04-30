package video

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/cache"
	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

const (
	chunkAlign = 4096
	maxChunk   = 1024 * 1024 // 1 MiB — Telegram's per-call limit
)

// StreamServer wires together the dependencies needed to serve a TG video
// over HTTP with Range support.
type StreamServer struct {
	Cfg   *config.Config
	DB    *db.DB
	TG    *tgmanager.Manager
	Cache *cache.Manager
}

// Handler returns an http.HandlerFunc that serves /api/videos/{id}/stream.
func (s *StreamServer) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _ := web.UserIDFromContext(r.Context())
		idStr := URLParamFromRequest(r, "id")
		vid, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid video id"))
			return
		}
		v, err := s.DB.VideoByID(r.Context(), vid, uid)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "video not found"))
			return
		}

		// Fast path: cached file present and complete.
		if path, ok := s.Cache.CompletePathFor(r.Context(), v.TGDocID); ok {
			s.Cache.Touch(r.Context(), v.TGDocID)
			http.ServeFile(w, r, path)
			return
		}

		s.serveFromTelegram(w, r, v)
	}
}

func (s *StreamServer) serveFromTelegram(w http.ResponseWriter, r *http.Request, v *db.Video) {
	cli, err := s.TG.ClientFor(v.UserID)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "tg_unavailable", "telegram client not ready"))
		return
	}

	if v.SizeBytes <= 0 {
		httpx.WriteError(w, httpx.Errorf(http.StatusInternalServerError, "no_size", "video size unknown"))
		return
	}

	start, end, ok := parseRange(r.Header.Get("Range"), v.SizeBytes)
	if !ok {
		w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(v.SizeBytes, 10))
		httpx.WriteError(w, httpx.Errorf(http.StatusRequestedRangeNotSatisfiable, "bad_range", "invalid Range header"))
		return
	}

	contentLen := end - start + 1
	mime := v.Mime
	if mime == "" {
		mime = "video/mp4"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(contentLen, 10))

	status := http.StatusOK
	if r.Header.Get("Range") != "" {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, v.SizeBytes))
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)

	// HEAD request: headers only.
	if r.Method == http.MethodHead {
		return
	}

	// Open a TeeWriter into a partial cache file if this is a favorite.
	var sink io.Writer = w
	if s.Cache != nil {
		if tee, finish, ok := s.Cache.MaybeTee(r.Context(), v, start); ok {
			defer finish()
			sink = io.MultiWriter(w, tee)
		}
	}

	if err := s.streamRange(r.Context(), cli.API, v, start, end, sink); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("stream failed", "video_id", v.ID, "err", err)
		}
	}
}

// streamRange reads [start, end] from Telegram and writes it to dst.
// It handles 4KB alignment (asks the server for chunks aligned down) and
// trims leading/trailing bytes before writing.
func (s *StreamServer) streamRange(ctx context.Context, api *tg.Client, v *db.Video, start, end int64, dst io.Writer) error {
	// First chunk offset must align down to chunkAlign for Telegram.
	cursor := start
	refreshed := false

	for cursor <= end {
		alignedOffset := cursor - (cursor % chunkAlign)
		prefixSkip := cursor - alignedOffset
		// Compute the limit (4KB-aligned, never exceeding maxChunk).
		want := end - alignedOffset + 1
		if want > maxChunk {
			want = maxChunk
		}
		if want%chunkAlign != 0 {
			want += chunkAlign - (want % chunkAlign)
		}

		req := &tg.UploadGetFileRequest{
			Precise: true,
			Location: &tg.InputDocumentFileLocation{
				ID:            v.TGDocID,
				AccessHash:    v.AccessHash,
				FileReference: v.FileReference,
			},
			Offset: alignedOffset,
			Limit:  int(want),
		}
		resp, err := api.UploadGetFile(ctx, req)
		if err != nil {
			if !refreshed && tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
				refreshed = true
				if rerr := RefreshFileReference(ctx, s.DB, s.TG, v); rerr == nil {
					continue
				} else {
					return fmt.Errorf("refresh file_reference: %w", rerr)
				}
			}
			return fmt.Errorf("upload.getFile: %w", err)
		}

		uf, ok := resp.(*tg.UploadFile)
		if !ok {
			return fmt.Errorf("unexpected response type %T (CDN unsupported)", resp)
		}
		data := uf.Bytes
		if len(data) == 0 {
			break // EOF.
		}
		// Trim front (alignment skip) and back (range tail).
		if int64(len(data)) > prefixSkip {
			data = data[prefixSkip:]
		} else {
			data = nil
		}
		// Tail trim: don't write past `end`.
		want64 := end - cursor + 1
		if int64(len(data)) > want64 {
			data = data[:want64]
		}
		if len(data) > 0 {
			if _, err := dst.Write(data); err != nil {
				return err
			}
			cursor += int64(len(data))
		}
		// Telegram returned fewer bytes than requested → EOF reached.
		if int64(len(uf.Bytes)) < int64(want) {
			break
		}
	}
	return nil
}

// parseRange parses the HTTP Range header value (only `bytes=` supported).
// Returns inclusive [start, end]. If the header is empty, returns [0, size-1].
func parseRange(h string, size int64) (start, end int64, ok bool) {
	if size <= 0 {
		return 0, 0, false
	}
	if h == "" {
		return 0, size - 1, true
	}
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(h, "bytes=")
	// Only one range supported.
	if i := strings.IndexByte(spec, ','); i >= 0 {
		spec = spec[:i]
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	startStr := spec[:dash]
	endStr := spec[dash+1:]

	if startStr == "" {
		// Suffix form: bytes=-N → last N bytes.
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}

	s, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || s < 0 || s >= size {
		return 0, 0, false
	}
	e := size - 1
	if endStr != "" {
		ev, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || ev < s {
			return 0, 0, false
		}
		if ev < e {
			e = ev
		}
	}
	return s, e, true
}

