// Package media serves the two file types that aren't HTTP-range-streamed
// video: full-size images, and the grid thumbnails of both images and videos.
//
// Images are small enough that there is no Range/seek story — the handler just
// makes sure the file is on disk (downloading it inline the first time) and
// hands it to http.ServeFile. Thumbnails follow the same shape but are fetched
// lazily, so only what someone actually scrolls past ever costs an RPC.
package media

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hanfeilong/onlineplaytgvideo/internal/auth/web"
	"github.com/hanfeilong/onlineplaytgvideo/internal/cache"
	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/httpx"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
	"github.com/hanfeilong/onlineplaytgvideo/internal/video"
)

// fetchTimeout bounds one inline download. Images and thumbs are small; a
// stuck DC connection should fail the request rather than hang the browser's
// connection slot.
const fetchTimeout = 60 * time.Second

type Server struct {
	Cfg   *config.Config
	DB    *db.DB
	TG    *tgmanager.Manager
	Cache *cache.Manager

	// inflight serialises concurrent fetches of the same file. A media grid
	// requests dozens of thumbnails at once and React strict-mode double-mounts
	// them, so without this the same photo gets downloaded several times over.
	inflight sync.Map // string -> *sync.Mutex
}

func (s *Server) lock(key string) func() {
	mu, _ := s.inflight.LoadOrStore(key, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// PhotoFileHandler serves the full-size image: GET /api/photos/{id}/file
func (s *Server) PhotoFileHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _ := web.UserIDFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid photo id"))
			return
		}
		p, err := s.DB.PhotoByID(r.Context(), id, uid)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "photo not found"))
			return
		}

		// Fast path: already on disk.
		if p.TGPhotoID != 0 {
			if path, ok := s.Cache.CompletePathFor(r.Context(), db.MediaKindPhoto, p.TGPhotoID); ok {
				s.Cache.Touch(r.Context(), db.MediaKindPhoto, p.TGPhotoID)
				serveImage(w, r, path)
				return
			}
		}

		unlock := s.lock(fmt.Sprintf("photo:%d", id))
		defer unlock()
		// Another request may have finished it while we waited on the lock.
		if p.TGPhotoID != 0 {
			if path, ok := s.Cache.CompletePathFor(r.Context(), db.MediaKindPhoto, p.TGPhotoID); ok {
				serveImage(w, r, path)
				return
			}
		}

		ctx, cancel := contextWithTimeout(r, fetchTimeout)
		defer cancel()
		if err := s.ensurePhotoLocator(ctx, p); err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_resolve", err.Error()))
			return
		}
		path, err := s.Cache.DownloadPhoto(ctx, p)
		if err != nil {
			slog.Warn("photo download failed", "photo_id", p.ID, "err", err)
			httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_download", err.Error()))
			return
		}
		serveImage(w, r, path)
	}
}

// PhotoThumbHandler serves the grid thumbnail: GET /api/photos/{id}/thumb
func (s *Server) PhotoThumbHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _ := web.UserIDFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid photo id"))
			return
		}
		p, err := s.DB.PhotoByID(r.Context(), id, uid)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "photo not found"))
			return
		}
		dst := s.thumbPath(db.MediaKindPhoto, p.ID)
		if serveExisting(w, r, dst) {
			return
		}

		unlock := s.lock(fmt.Sprintf("pthumb:%d", id))
		defer unlock()
		if serveExisting(w, r, dst) {
			return
		}

		ctx, cancel := contextWithTimeout(r, fetchTimeout)
		defer cancel()
		if err := s.ensurePhotoLocator(ctx, p); err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_resolve", err.Error()))
			return
		}
		size := p.ThumbSize
		if size == "" {
			size = p.SizeType // no small size on the ladder — fall back to the original
		}
		ch, err := s.DB.ChannelByID(ctx, p.ChannelID, p.UserID)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		cli, err := s.TG.ClientForSession(ch.TGSessionID)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "tg_unavailable", "telegram client not ready"))
			return
		}
		if err := s.fetchThumb(ctx, dst, func(tmp string) error {
			_, derr := s.Cache.DownloadPhotoSize(ctx, cli.APIForDC(p.DCID), p, size, tmp)
			return derr
		}); err != nil {
			slog.Warn("photo thumb failed", "photo_id", p.ID, "err", err)
			httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_thumb", err.Error()))
			return
		}
		if rel, err := filepath.Rel(s.Cfg.CacheDir, dst); err == nil {
			_ = s.DB.SetPhotoThumbPath(ctx, p.ID, rel)
		}
		serveImage(w, r, dst)
	}
}

// VideoThumbHandler serves a video's poster frame: GET /api/videos/{id}/thumb
//
// Telegram attaches a small JPEG to most video documents; which size letter to
// ask for is recorded as videos.thumb_size during sync. Rows that predate that
// (or JSON imports) resolve their locator here on first request, exactly like
// first playback does.
func (s *Server) VideoThumbHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _ := web.UserIDFromContext(r.Context())
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusBadRequest, "bad_id", "invalid video id"))
			return
		}
		v, err := s.DB.VideoByID(r.Context(), id, uid)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "not_found", "video not found"))
			return
		}
		dst := s.thumbPath(db.MediaKindVideo, v.ID)
		if serveExisting(w, r, dst) {
			return
		}

		unlock := s.lock(fmt.Sprintf("vthumb:%d", id))
		defer unlock()
		if serveExisting(w, r, dst) {
			return
		}

		ctx, cancel := contextWithTimeout(r, fetchTimeout)
		defer cancel()
		if v.TGDocID == 0 || len(v.FileReference) == 0 {
			if err := video.RefreshFileReference(ctx, s.DB, s.TG, v); err != nil {
				httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_resolve", err.Error()))
				return
			}
		}
		if v.ThumbSize == "" {
			httpx.WriteError(w, httpx.Errorf(http.StatusNotFound, "no_thumb", "该视频没有可用缩略图"))
			return
		}
		ch, err := s.DB.ChannelByID(ctx, v.ChannelID, v.UserID)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		cli, err := s.TG.ClientForSession(ch.TGSessionID)
		if err != nil {
			httpx.WriteError(w, httpx.Errorf(http.StatusServiceUnavailable, "tg_unavailable", "telegram client not ready"))
			return
		}
		if err := s.fetchThumb(ctx, dst, func(tmp string) error {
			_, derr := s.Cache.DownloadDocThumb(ctx, cli.APIForDC(v.DCID), v, v.ThumbSize, tmp)
			return derr
		}); err != nil {
			slog.Warn("video thumb failed", "video_id", v.ID, "err", err)
			httpx.WriteError(w, httpx.Errorf(http.StatusBadGateway, "tg_thumb", err.Error()))
			return
		}
		if rel, err := filepath.Rel(s.Cfg.CacheDir, dst); err == nil {
			_ = s.DB.SetVideoThumbPath(ctx, v.ID, rel)
		}
		serveImage(w, r, dst)
	}
}

// ensurePhotoLocator resolves a photo's TG locator if it is missing (a
// JSON-imported row is stored without one).
func (s *Server) ensurePhotoLocator(ctx context.Context, p *db.Photo) error {
	if p.TGPhotoID != 0 && len(p.FileReference) > 0 && p.SizeType != "" {
		return nil
	}
	return video.RefreshPhotoReference(ctx, s.DB, s.TG, p)
}

func (s *Server) thumbPath(kind string, id int64) string {
	return filepath.Join(s.Cache.ThumbDir(), fmt.Sprintf("%s_%d.jpg", kind, id))
}

// fetchThumb downloads into a temp file and renames it into place, so a failed
// or half-finished download never leaves a truncated image that would then be
// served forever.
func (s *Server) fetchThumb(ctx context.Context, dst string, download func(tmp string) error) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".dl"
	if err := download(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if fi, err := os.Stat(tmp); err != nil || fi.Size() == 0 {
		_ = os.Remove(tmp)
		if err != nil {
			return err
		}
		return fmt.Errorf("telegram returned an empty thumbnail")
	}
	return os.Rename(tmp, dst)
}

// contextWithTimeout bounds one inline fetch while still inheriting the
// request's cancellation (browser navigated away → stop downloading).
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func serveExisting(w http.ResponseWriter, r *http.Request, path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return false
	}
	serveImage(w, r, path)
	return true
}

func serveImage(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Content-Type", "image/jpeg")
	// Cached files are immutable (keyed by Telegram's own id), so let the
	// browser hold them: a media grid otherwise re-requests every tile on
	// every scroll back.
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeFile(w, r, path)
}
