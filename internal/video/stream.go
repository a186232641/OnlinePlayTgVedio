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
	"time"

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
	// Telegram's upload.getFile requires `offset % limit == 0`. We always
	// fetch in 1 MiB blocks aligned to 1 MiB boundaries — simplifies the
	// loop and avoids LIMIT_INVALID when the user seeks to an offset that's
	// 4 KiB-aligned but not 1 MiB-aligned.
	chunkSize = 1024 * 1024 // 1 MiB
	// maxStream caps the unknown-size path. Bigger than any real video; used
	// as a sentinel "stream until TG returns empty (EOF)".
	maxStream = 1 << 60
	// chunkTimeout bounds a single upload.getFile attempt. A stuck TG connection
	// (dead/zombie session, unreachable file DC) otherwise hangs until the
	// browser gives up tens of seconds later, freezing playback with no error.
	// Failing fast surfaces a logged DeadlineExceeded instead.
	chunkTimeout = 20 * time.Second
	// fetchAttempts / retryBackoff: retry a transient block fetch this many
	// times. The first access to a file's DC must dial it, which intermittently
	// times out on a flaky link; a retry usually lands the (then-cached) DC
	// connection. Worst case for a truly dead DC ≈ fetchAttempts·chunkTimeout.
	fetchAttempts = 3
	retryBackoff  = time.Second
	// readAheadWorkers is how many 1 MiB blocks a bounded Range request fetches
	// from Telegram concurrently. Overlapping round-trips lifts throughput on
	// large videos; kept small so a single playback doesn't hammer TG (FLOOD).
	readAheadWorkers = 4
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

		slog.Info("stream request",
			"method", r.Method,
			"video_id", v.ID,
			"tg_msg_id", v.TGMsgID,
			"tg_doc_id", v.TGDocID,
			"file_size", v.FileSize,
			"has_ref", len(v.FileReference) > 0,
			"range", r.Header.Get("Range"),
		)

		// Fast path: cached file present and complete.
		if v.TGDocID != 0 {
			if path, ok := s.Cache.CompletePathFor(r.Context(), v.TGDocID); ok {
				s.Cache.Touch(r.Context(), v.TGDocID)
				http.ServeFile(w, r, path)
				return
			}
		}

		s.serveFromTelegram(w, r, v)
	}
}

func (s *StreamServer) serveFromTelegram(w http.ResponseWriter, r *http.Request, v *db.Video) {
	ch, err := s.DB.ChannelByID(r.Context(), v.ChannelID, v.UserID)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "channel_lookup", "channel not found"))
		return
	}
	cli, err := s.TG.ClientForSession(ch.TGSessionID)
	if err != nil {
		httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "tg_unavailable", "telegram client not ready"))
		return
	}

	// JSON-imported rows start with TGDocID=0 / empty FileReference; resolve
	// the locator on first play by hitting channels.getMessages.
	if v.TGDocID == 0 || len(v.FileReference) == 0 {
		if err := RefreshFileReference(r.Context(), s.DB, s.TG, v); err != nil {
			slog.Warn("stream resolve failed", "video_id", v.ID, "tg_msg_id", v.TGMsgID, "err", err)
			httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_resolve", err.Error()))
			return
		}
	}

	// Kick off a background multi-threaded full-file download into the cache.
	// Idempotent: no-op if already cached or queued. Future plays/seeks then
	// serve straight from disk instead of re-streaming from Telegram.
	if s.Cache != nil {
		s.Cache.EnsureCached(v.ID)
	}

	mime := v.MimeType
	if mime == "" {
		mime = "video/mp4"
	}
	w.Header().Set("Content-Type", mime)

	// Unknown-size path: TG didn't include Document.Size for this doc (some
	// videos uploaded by 3rd-party clients lack it). Browsers can still
	// progressively play but lose seek; we ignore any Range header and stream
	// from 0 until TG returns an empty chunk (EOF).
	if v.FileSize <= 0 {
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		if err := s.streamRange(r.Context(), cli.API, v, 0, maxStream, w); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Warn("stream(unknown size) failed", "video_id", v.ID, "err", err)
			}
		}
		return
	}

	start, end, ok := parseRange(r.Header.Get("Range"), v.FileSize)
	if !ok {
		w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(v.FileSize, 10))
		httpx.WriteError(w, httpx.Errorf(http.StatusRequestedRangeNotSatisfiable, "bad_range", "invalid Range header"))
		return
	}

	contentLen := end - start + 1
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(contentLen, 10))

	status := http.StatusOK
	if r.Header.Get("Range") != "" {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, v.FileSize))
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)

	// HEAD request: headers only.
	if r.Method == http.MethodHead {
		return
	}

	if err := s.streamRange(r.Context(), cli.API, v, start, end, w); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("stream failed", "video_id", v.ID, "err", err)
		}
	}
}

// streamRange reads [start, end] from Telegram and writes it to dst, in order.
// Telegram requires offset to be divisible by limit; we always request 1 MiB
// blocks aligned to 1 MiB boundaries, then trim the leading (start-aligned)
// bytes off the first block and the tail off the last so the caller sees
// exactly the bytes they asked for.
//
// Bounded ranges (the common Range-request case) go through streamWindowed,
// which fetches several blocks concurrently to overlap TG round-trips and lift
// throughput on large/high-bitrate videos. The unknown-size path (end ==
// maxStream) can't plan blocks ahead of EOF, so it stays sequential.
func (s *StreamServer) streamRange(ctx context.Context, api *tg.Client, v *db.Video, start, end int64, dst io.Writer) error {
	if end >= maxStream {
		return s.streamSequential(ctx, api, v, start, end, dst)
	}
	// Bounded range. streamWindowed returns the bytes it managed to write so a
	// mid-stream file_reference refresh can resume from the exact cursor (the
	// client stream can't be rewound).
	cursor := start
	refreshed := false
	fetch := func(c context.Context, offset int64) ([]byte, error) { return fetchBlock(c, api, v, offset) }
	for cursor <= end {
		n, err := streamWindowed(ctx, fetch, cursor, end, dst, v.ID)
		cursor += n
		if err == nil {
			return nil
		}
		if !refreshed && tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
			refreshed = true
			if rerr := RefreshFileReference(ctx, s.DB, s.TG, v); rerr != nil {
				return fmt.Errorf("refresh file_reference: %w", rerr)
			}
			continue
		}
		return err
	}
	return nil
}

// streamWindowed fetches the aligned 1 MiB blocks covering [start, end] with a
// small pool of concurrent upload.getFile calls and writes them to dst in
// order. Worker w owns the stripe of blocks {w, w+W, w+2W, …} and sends each
// over its own size-1 channel, so each worker runs at most one block ahead of
// what the in-order writer has consumed — bounding read-ahead to ~2·W blocks
// of memory and in-flight RPCs. Returns bytes written (for resume) and the
// first error seen. fetch retrieves one aligned 1 MiB block at the given
// offset (it's the only TG dependency, so this stays unit-testable).
func streamWindowed(ctx context.Context, fetch func(context.Context, int64) ([]byte, error), start, end int64, dst io.Writer, videoID int64) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // unblocks workers when the writer stops early (error/EOF)

	alignedStart := start - start%chunkSize

	// Don't spin up more workers than there are blocks to fetch — a tiny range
	// (e.g. the 16-byte container probe) needs just one, not four 1 MiB fetches.
	nBlocks := (end-end%chunkSize-alignedStart)/chunkSize + 1
	workers := readAheadWorkers
	if int64(workers) > nBlocks {
		workers = int(nBlocks)
	}

	type result struct {
		data []byte
		err  error
	}
	chans := make([]chan result, workers)
	for i := range chans {
		chans[i] = make(chan result, 1)
	}

	// v's locator is only mutated by RefreshFileReference between windowed
	// passes (never during one), so concurrent reads here are race-free.
	for w := 0; w < workers; w++ {
		go func(w int) {
			for offset := alignedStart + int64(w)*chunkSize; offset <= end; offset += int64(workers) * chunkSize {
				data, err := fetch(ctx, offset)
				select {
				case chans[w] <- result{data: data, err: err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}(w)
	}

	remaining := end - start + 1
	var written int64
	for i := 0; ; i++ {
		var res result
		select {
		case res = <-chans[i%workers]:
		case <-ctx.Done():
			return written, ctx.Err()
		}
		if res.err != nil {
			return written, fmt.Errorf("upload.getFile: %w", res.err)
		}
		data := res.data
		if i == 0 {
			if skip := start - alignedStart; int64(len(data)) > skip {
				data = data[skip:]
			} else {
				data = nil
			}
			if len(res.data) >= 8 {
				slog.Info("stream first-chunk header",
					"video_id", videoID, "chunk_size", len(res.data),
					"hex8", fmt.Sprintf("%02x %02x %02x %02x %02x %02x %02x %02x",
						res.data[0], res.data[1], res.data[2], res.data[3],
						res.data[4], res.data[5], res.data[6], res.data[7]))
			}
		}
		if int64(len(data)) > remaining {
			data = data[:remaining]
		}
		if len(data) > 0 {
			if _, err := dst.Write(data); err != nil {
				return written, err
			}
			written += int64(len(data))
			remaining -= int64(len(data))
		}
		if remaining <= 0 {
			return written, nil
		}
		// A short/empty block means Telegram has no more data (EOF) — stop
		// rather than spin waiting for blocks past the file's end.
		if len(res.data) < chunkSize {
			slog.Info("stream EOF (short chunk)", "video_id", videoID, "written", written)
			return written, nil
		}
	}
}

// fetchBlock fetches a single aligned 1 MiB block at offset, bounded by
// chunkTimeout per attempt and retried on transient failures. It returns the
// raw bytes (possibly short at EOF); callers do the alignment/tail trimming.
//
// Retry exists because file blocks live on different Telegram DCs, and the
// first access to a not-yet-connected DC must export auth + dial it — a step
// that intermittently times out on a flaky VPS↔TG link ("create connection to
// DC N … i/o timeout"). gotd caches a DC connection once established, so a
// retry that lands the connection makes every later block on that DC fast.
func fetchBlock(ctx context.Context, api *tg.Client, v *db.Video, offset int64) ([]byte, error) {
	req := &tg.UploadGetFileRequest{
		Precise: true,
		Location: &tg.InputDocumentFileLocation{
			ID:            v.TGDocID,
			AccessHash:    v.AccessHash,
			FileReference: v.FileReference,
		},
		Offset: offset,
		Limit:  chunkSize,
	}
	var lastErr error
	for attempt := 0; attempt < fetchAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(retryBackoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		callCtx, cancel := context.WithTimeout(ctx, chunkTimeout)
		resp, err := api.UploadGetFile(callCtx, req)
		cancel()
		if err == nil {
			uf, ok := resp.(*tg.UploadFile)
			if !ok {
				return nil, fmt.Errorf("unexpected response type %T (CDN unsupported)", resp)
			}
			return uf.Bytes, nil
		}
		// Caller gone, or a stale ref the upstream loop must refresh — don't retry.
		if !transient(err) {
			return nil, err
		}
		lastErr = err
		slog.Warn("fetchBlock retry",
			"video_id", v.ID, "offset", offset, "attempt", attempt+1, "err", err)
	}
	return nil, lastErr
}

// transient reports whether err is worth retrying: a timeout / connection blip
// (incl. our per-call deadline and cross-DC connect failures), but NOT a caller
// cancellation (client disconnected) or a stale file_reference (the caller
// handles that by refreshing the locator).
func transient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	return !tgerr.Is(err, "FILE_REFERENCE_EXPIRED")
}

// streamSequential is the original one-block-at-a-time reader, kept for the
// unknown-size path where the EOF offset isn't known up front.
func (s *StreamServer) streamSequential(ctx context.Context, api *tg.Client, v *db.Video, start, end int64, dst io.Writer) error {
	cursor := start
	refreshed := false

	for cursor <= end {
		alignedOffset := cursor - (cursor % chunkSize)
		prefixSkip := cursor - alignedOffset

		req := &tg.UploadGetFileRequest{
			Precise: true,
			Location: &tg.InputDocumentFileLocation{
				ID:            v.TGDocID,
				AccessHash:    v.AccessHash,
				FileReference: v.FileReference,
			},
			Offset: alignedOffset,
			Limit:  chunkSize,
		}
		callCtx, cancel := context.WithTimeout(ctx, chunkTimeout)
		resp, err := api.UploadGetFile(callCtx, req)
		cancel()
		if err != nil {
			slog.Warn("upload.getFile failed",
				"video_id", v.ID, "offset", alignedOffset, "limit", chunkSize, "err", err)
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
			slog.Warn("upload.getFile returned non-UploadFile (CDN?)",
				"video_id", v.ID, "type", fmt.Sprintf("%T", resp))
			return fmt.Errorf("unexpected response type %T (CDN unsupported)", resp)
		}
		data := uf.Bytes
		if cursor == start && len(data) >= 8 {
			slog.Info("stream first-chunk header",
				"video_id", v.ID,
				"chunk_size", len(data),
				"hex8", fmt.Sprintf("%02x %02x %02x %02x %02x %02x %02x %02x",
					data[0], data[1], data[2], data[3], data[4], data[5], data[6], data[7]),
			)
		}
		if len(data) == 0 {
			slog.Info("stream EOF (empty chunk)", "video_id", v.ID, "cursor", cursor)
			break
		}
		// Trim front (alignment skip) and back (range tail).
		if int64(len(data)) > prefixSkip {
			data = data[prefixSkip:]
		} else {
			data = nil
		}
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
		if int64(len(uf.Bytes)) < int64(chunkSize) {
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

