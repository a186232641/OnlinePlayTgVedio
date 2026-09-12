package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"

	"github.com/hanfeilong/onlineplaytgvideo/internal/config"
	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
	"github.com/hanfeilong/onlineplaytgvideo/internal/tgmanager"
)

const (
	// cachePartSize is the per-request block for downloads. Telegram caps a
	// single upload.getFile at 1 MiB and requires the part size to divide it.
	cachePartSize = 1024 * 1024 // 1 MiB
	// cacheMaxThreads caps the parallel connections used per file download.
	cacheMaxThreads = 8
)

// threadLevels scales the download thread count by file size (mirrors tdl's
// BestThreads): tiny files don't benefit from many connections.
var threadLevels = []struct {
	threads int
	size    int64
}{
	{1, 1 << 20},
	{2, 5 << 20},
	{4, 20 << 20},
	{8, 50 << 20},
}

func bestThreads(size int64, max int) int {
	for _, l := range threadLevels {
		if size < l.size {
			return min(l.threads, max)
		}
	}
	return max
}

// cacheKey identifies one cached file. Videos are keyed by Telegram document
// id and images by photo id — two independent id spaces, so the kind has to be
// part of the key everywhere (on disk, in cache_entries, in the dedup map).
type cacheKey struct {
	kind string
	id   int64
}

// cacheJob is one queued download. The key lets the worker dedup and release
// the in-flight slot even if the per-row lookup later fails.
type cacheJob struct {
	key     cacheKey
	mediaID int64 // videos.id / photos.id
}

// Manager owns the on-disk cache directory, the background download worker, and
// the periodic LRU eviction job. Every played video is cached (multi-threaded,
// tdl-style); favorites are pinned so eviction never drops them.
type Manager struct {
	cfg *config.Config
	db  *db.DB
	tg  *tgmanager.Manager

	// RefreshLocator re-fetches a video's fresh file_reference when a download
	// fails with FILE_REFERENCE_EXPIRED. Wired in main.go to video.RefreshFileReference
	// (a function field avoids a cache↔video import cycle). May be nil.
	RefreshLocator func(ctx context.Context, v *db.Video) error
	// RefreshPhotoLocator is the same hook for images. May be nil.
	RefreshPhotoLocator func(ctx context.Context, p *db.Photo) error

	queue chan cacheJob

	mu       sync.Mutex
	queued   map[cacheKey]struct{} // files currently queued/in-flight
	stopOnce sync.Once
	stopCh   chan struct{}
}

type diskFile struct {
	Key   cacheKey
	Path  string
	Bytes int64
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Manager {
	return &Manager{
		cfg:    cfg,
		db:     database,
		tg:     mgr,
		queue:  make(chan cacheJob, 256),
		queued: map[cacheKey]struct{}{},
		stopCh: make(chan struct{}),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	for _, d := range []string{m.videoDir(), m.photoDir(), m.ThumbDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := m.cleanPartials(); err != nil {
		slog.Warn("clean partial caches", "err", err)
	}
	go m.workerLoop(ctx)
	go m.gcLoop(ctx)
	return nil
}

func (m *Manager) Stop()            { m.stopOnce.Do(func() { close(m.stopCh) }) }
func (m *Manager) videoDir() string { return filepath.Join(m.cfg.CacheDir, "videos") }
func (m *Manager) photoDir() string { return filepath.Join(m.cfg.CacheDir, "photos") }

// ThumbDir holds the small grid thumbnails. They are fetched on demand by the
// media handlers (not by this manager) but live under the same cache root, so
// their bytes count towards CACHE_CAP_GB.
func (m *Manager) ThumbDir() string { return filepath.Join(m.cfg.CacheDir, "thumbs") }

// tmpDir is per kind so a half-written photo can't collide with a video.
func (m *Manager) tmpDir(kind string) string { return filepath.Join(m.dirFor(kind), "tmp") }

func (m *Manager) dirFor(kind string) string {
	if kind == db.MediaKindPhoto {
		return m.photoDir()
	}
	return m.videoDir()
}

func (m *Manager) pathFor(key cacheKey) string {
	return filepath.Join(m.dirFor(key.kind), fmt.Sprintf("%d.bin", key.id))
}

func (m *Manager) relPathFor(key cacheKey) string {
	dir := "videos"
	if key.kind == db.MediaKindPhoto {
		dir = "photos"
	}
	return relPath(dir, fmt.Sprintf("%d.bin", key.id))
}

// CompletePathFor returns the on-disk path of a fully cached file, if any.
func (m *Manager) CompletePathFor(ctx context.Context, kind string, id int64) (string, bool) {
	c, err := m.db.GetCacheEntry(ctx, kind, id)
	if err != nil || !c.Completed {
		return "", false
	}
	abs := filepath.Join(m.cfg.CacheDir, c.FilePath)
	if _, err := os.Stat(abs); err != nil {
		return "", false
	}
	return abs, true
}

func (m *Manager) Touch(ctx context.Context, kind string, id int64) {
	_ = m.db.TouchCache(ctx, kind, id)
}

// InvalidateCorrupt drops a cached file that failed a serve-time integrity check
// (wrong on-disk size) and resets its entry so the next play re-downloads it.
// The pinned flag is preserved so favorites stay pinned across the re-download.
func (m *Manager) InvalidateCorrupt(ctx context.Context, kind string, id int64) {
	if m == nil || id == 0 {
		return
	}
	key := cacheKey{kind: kind, id: id}
	if err := os.Remove(m.pathFor(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("invalidate corrupt cache: remove file", "kind", kind, "doc_id", id, "err", err)
	}
	if err := m.db.MarkCacheIncomplete(ctx, kind, id); err != nil {
		slog.Warn("invalidate corrupt cache: mark incomplete", "kind", kind, "doc_id", id, "err", err)
	}
}

// EnsureCached schedules a background full-file download for a played video so
// future plays/seeks serve from disk. The entry is unpinned (LRU-evictable).
// No-op if already cached, already queued, or the locator isn't resolved yet.
func (m *Manager) EnsureCached(videoID int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	docID, err := m.db.LookupDocByVideoID(ctx, videoID)
	if err != nil || docID == 0 {
		return
	}
	key := cacheKey{kind: db.MediaKindVideo, id: docID}
	if c, err := m.db.GetCacheEntry(ctx, key.kind, key.id); err == nil && c.Completed {
		return
	}
	// Placeholder so the entry exists; bytes=0/completed=false keeps it out of
	// the total and LRU until the download finishes. Upsert's OR semantics never
	// un-pin an already-favorited entry.
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		Kind:     key.kind,
		TGDocID:  key.id,
		FilePath: m.relPathFor(key),
		Pinned:   false,
	})
	m.submit(key, videoID)
}

// EnsurePhotoCached is EnsureCached for an image: queue a background download so
// the next view of the same photo is served off disk.
func (m *Manager) EnsurePhotoCached(photoID, tgPhotoID int64) {
	if m == nil || tgPhotoID == 0 {
		return
	}
	ctx := context.Background()
	key := cacheKey{kind: db.MediaKindPhoto, id: tgPhotoID}
	if c, err := m.db.GetCacheEntry(ctx, key.kind, key.id); err == nil && c.Completed {
		return
	}
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		Kind:     key.kind,
		TGDocID:  key.id,
		FilePath: m.relPathFor(key),
		Pinned:   false,
	})
	m.submit(key, photoID)
}

// EnqueueFavorite is called when a user adds a favorite: pin the doc and (if not
// already cached) schedule the same background download.
func (m *Manager) EnqueueFavorite(userID, videoID int64) {
	ctx := context.Background()
	docID, completed, err := m.db.PinByVideoID(ctx, videoID)
	if err != nil {
		slog.Warn("pin cache for favorite", "video_id", videoID, "err", err)
		return
	}
	if completed || docID == 0 {
		return
	}
	key := cacheKey{kind: db.MediaKindVideo, id: docID}
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		Kind:     key.kind,
		TGDocID:  key.id,
		FilePath: m.relPathFor(key),
		Pinned:   true,
	})
	m.submit(key, videoID)
}

// HandleUnfavorite is called when a favorite is removed. It unpins the cache
// entry only if no other user still has it favorited.
func (m *Manager) HandleUnfavorite(userID, videoID int64) {
	_ = m.db.UnpinIfNotFavorited(context.Background(), videoID)
}

// EnqueuePhotoFavorite / HandlePhotoUnfavorite mirror the video versions:
// favoriting an image pins its file so the LRU keeps it.
func (m *Manager) EnqueuePhotoFavorite(userID, photoID int64) {
	ctx := context.Background()
	tgPhotoID, completed, err := m.db.PinByPhotoID(ctx, photoID)
	if err != nil {
		slog.Warn("pin cache for photo favorite", "photo_id", photoID, "err", err)
		return
	}
	if completed || tgPhotoID == 0 {
		return
	}
	key := cacheKey{kind: db.MediaKindPhoto, id: tgPhotoID}
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		Kind:     key.kind,
		TGDocID:  key.id,
		FilePath: m.relPathFor(key),
		Pinned:   true,
	})
	m.submit(key, photoID)
}

func (m *Manager) HandlePhotoUnfavorite(userID, photoID int64) {
	_ = m.db.UnpinPhotoIfNotFavorited(context.Background(), photoID)
}

// submit enqueues a download job, deduped by (kind, id). A full queue drops the
// request (a later play or GC pass re-submits it).
func (m *Manager) submit(key cacheKey, mediaID int64) {
	if key.id == 0 {
		return
	}
	m.mu.Lock()
	if _, ok := m.queued[key]; ok {
		m.mu.Unlock()
		return
	}
	m.queued[key] = struct{}{}
	m.mu.Unlock()
	select {
	case m.queue <- cacheJob{key: key, mediaID: mediaID}:
	default:
		m.mu.Lock()
		delete(m.queued, key)
		m.mu.Unlock()
	}
}

func (m *Manager) workerLoop(ctx context.Context) {
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case job := <-m.queue:
			m.runDownload(ctx, job)
			m.mu.Lock()
			delete(m.queued, job.key)
			m.mu.Unlock()
		}
	}
}

func (m *Manager) runDownload(ctx context.Context, job cacheJob) {
	if job.key.kind == db.MediaKindPhoto {
		p, err := m.db.PhotoByIDAny(ctx, job.mediaID)
		if err != nil {
			slog.Warn("cache lookup photo", "photo_id", job.mediaID, "err", err)
			return
		}
		if _, err := m.DownloadPhoto(ctx, p); err != nil {
			slog.Warn("photo cache download failed", "photo_id", job.mediaID, "tg_photo_id", p.TGPhotoID, "err", err)
		}
		return
	}
	v, err := m.lookupVideoForDownload(ctx, job.mediaID)
	if err != nil {
		slog.Warn("cache lookup video", "video_id", job.mediaID, "err", err)
		return
	}
	if err := m.downloadDoc(ctx, v); err != nil {
		slog.Warn("cache download failed", "video_id", job.mediaID, "doc_id", v.TGDocID, "err", err)
	}
}

// downloadDoc downloads the whole document with the multi-threaded gotd
// downloader (the tdl template), atomically promotes the temp file, records the
// cache entry, and triggers eviction if the new file pushed us over the cap.
func (m *Manager) downloadDoc(ctx context.Context, v *db.Video) error {
	if v.TGDocID == 0 {
		return fmt.Errorf("video %d has no document locator", v.ID)
	}
	key := cacheKey{kind: db.MediaKindVideo, id: v.TGDocID}
	if c, err := m.db.GetCacheEntry(ctx, key.kind, key.id); err == nil && c.Completed {
		return nil
	}
	ch, err := m.db.ChannelByID(ctx, v.ChannelID, v.UserID)
	if err != nil {
		return err
	}
	cli, err := m.tg.ClientForSession(ch.TGSessionID)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(m.tmpDir(key.kind), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(m.tmpDir(key.kind), fmt.Sprintf("%d.dl", v.TGDocID))
	final := m.pathFor(key)

	// Download straight from the document's DC via the pool (avoids per-request
	// DC migration / IO timeouts under parallel load).
	size, err := m.downloadParallel(ctx, cli.APIForDC(v.DCID), v, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Integrity gate: a multi-threaded download that dropped bytes under a flaky
	// link leaves a truncated file. It would still stream fine straight from
	// Telegram (correct bytes), but served off disk the browser gets a corrupt
	// stream and fails to decode (MEDIA_ERR DECODE). Telegram's Document.Size ==
	// file_size, so an exact mismatch means the file is incomplete — discard it
	// and let the next play re-stream from TG + re-enqueue the download.
	if v.FileSize > 0 && size != v.FileSize {
		_ = os.Remove(tmp)
		return fmt.Errorf("download size mismatch for doc %d: got %d, want %d", v.TGDocID, size, v.FileSize)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	rel, _ := filepath.Rel(m.cfg.CacheDir, final)
	if err := m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		Kind:      key.kind,
		TGDocID:   key.id,
		FilePath:  rel,
		Bytes:     size,
		Completed: true,
	}); err != nil {
		return err
	}
	slog.Info("cache stored", "doc_id", v.TGDocID, "bytes", size)
	m.evictIfNeeded(ctx)
	return nil
}

// DownloadPhoto fetches the full-size image into the cache and returns its
// on-disk path. Unlike videos it is synchronous and single-threaded: images are
// small enough that the request handler can just wait for it, and there is no
// Range/seek story to optimise for.
//
// Safe to call concurrently for the same photo — the temp file is per photo id
// and the final rename is atomic, so the worst case is a duplicated download.
func (m *Manager) DownloadPhoto(ctx context.Context, p *db.Photo) (string, error) {
	if p.TGPhotoID == 0 || len(p.FileReference) == 0 {
		return "", fmt.Errorf("photo %d has no locator", p.ID)
	}
	key := cacheKey{kind: db.MediaKindPhoto, id: p.TGPhotoID}
	final := m.pathFor(key)
	if c, err := m.db.GetCacheEntry(ctx, key.kind, key.id); err == nil && c.Completed {
		if _, serr := os.Stat(final); serr == nil {
			return final, nil
		}
	}
	ch, err := m.db.ChannelByID(ctx, p.ChannelID, p.UserID)
	if err != nil {
		return "", err
	}
	cli, err := m.tg.ClientForSession(ch.TGSessionID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.tmpDir(key.kind), 0o755); err != nil {
		return "", err
	}
	tmp := filepath.Join(m.tmpDir(key.kind), fmt.Sprintf("%d.dl", p.TGPhotoID))

	size, err := m.downloadPhotoFile(ctx, cli.APIForDC(p.DCID), p, p.SizeType, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	rel, _ := filepath.Rel(m.cfg.CacheDir, final)
	if err := m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		Kind:      key.kind,
		TGDocID:   key.id,
		FilePath:  rel,
		Bytes:     size,
		Completed: true,
	}); err != nil {
		return "", err
	}
	slog.Info("photo cache stored", "photo_id", p.ID, "tg_photo_id", p.TGPhotoID, "bytes", size)
	m.evictIfNeeded(ctx)
	return final, nil
}

// DownloadDocThumb writes a document's thumbnail (videos carry a small JPEG)
// to dst. Thumbnails live outside the LRU-managed media dirs — they are tiny,
// and a grid that re-fetched them on every scroll would be far worse than
// keeping them — so this does not touch cache_entries.
func (m *Manager) DownloadDocThumb(ctx context.Context, api *tg.Client, v *db.Video, sizeType, dst string) (int64, error) {
	if v.TGDocID == 0 || sizeType == "" {
		return 0, fmt.Errorf("video %d has no thumbnail", v.ID)
	}
	f, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	loc := &tg.InputDocumentFileLocation{
		ID:            v.TGDocID,
		AccessHash:    v.AccessHash,
		FileReference: v.FileReference,
		ThumbSize:     sizeType,
	}
	_, derr := downloader.NewDownloader().WithPartSize(cachePartSize).
		Download(api, loc).Parallel(ctx, f)
	cerr := f.Close()
	if derr != nil || cerr != nil {
		if derr == nil {
			derr = cerr
		}
		return 0, derr
	}
	st, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// DownloadPhotoSize writes one size of a photo (full image or thumbnail) to the
// given path. Used directly by the thumbnail handler, which stores outside the
// LRU-managed media dirs.
func (m *Manager) DownloadPhotoSize(ctx context.Context, api *tg.Client, p *db.Photo, sizeType, dst string) (int64, error) {
	return m.downloadPhotoFile(ctx, api, p, sizeType, dst)
}

func (m *Manager) downloadPhotoFile(ctx context.Context, api *tg.Client, p *db.Photo, sizeType, dst string) (int64, error) {
	refreshed := false
	for {
		f, err := os.Create(dst)
		if err != nil {
			return 0, err
		}
		loc := &tg.InputPhotoFileLocation{
			ID:            p.TGPhotoID,
			AccessHash:    p.AccessHash,
			FileReference: p.FileReference,
			ThumbSize:     sizeType,
		}
		_, derr := downloader.NewDownloader().WithPartSize(cachePartSize).
			Download(api, loc).Parallel(ctx, f)
		cerr := f.Close()
		if derr == nil && cerr == nil {
			st, serr := os.Stat(dst)
			if serr != nil {
				return 0, serr
			}
			return st.Size(), nil
		}
		if derr == nil {
			derr = cerr
		}
		if !refreshed && tgerr.Is(derr, "FILE_REFERENCE_EXPIRED") && m.RefreshPhotoLocator != nil {
			refreshed = true
			if rerr := m.RefreshPhotoLocator(ctx, p); rerr != nil {
				return 0, fmt.Errorf("refresh photo file_reference: %w", rerr)
			}
			continue
		}
		return 0, derr
	}
}

// downloadParallel runs the threaded download into tmp. On FILE_REFERENCE_EXPIRED
// it refreshes the locator once and retries.
func (m *Manager) downloadParallel(ctx context.Context, api *tg.Client, v *db.Video, tmp string) (int64, error) {
	refreshed := false
	for {
		f, err := os.Create(tmp)
		if err != nil {
			return 0, err
		}
		loc := &tg.InputDocumentFileLocation{
			ID:            v.TGDocID,
			AccessHash:    v.AccessHash,
			FileReference: v.FileReference,
		}
		threads := bestThreads(v.FileSize, cacheMaxThreads)
		_, derr := downloader.NewDownloader().WithPartSize(cachePartSize).
			Download(api, loc).WithThreads(threads).
			Parallel(ctx, f)
		cerr := f.Close()
		if derr == nil && cerr == nil {
			st, serr := os.Stat(tmp)
			if serr != nil {
				return 0, serr
			}
			return st.Size(), nil
		}
		if derr == nil {
			derr = cerr
		}
		if !refreshed && tgerr.Is(derr, "FILE_REFERENCE_EXPIRED") && m.RefreshLocator != nil {
			refreshed = true
			if rerr := m.RefreshLocator(ctx, v); rerr != nil {
				return 0, fmt.Errorf("refresh file_reference: %w", rerr)
			}
			continue
		}
		return 0, derr
	}
}

func (m *Manager) lookupVideoForDownload(ctx context.Context, videoID int64) (*db.Video, error) {
	row := m.db.QueryRow(ctx, `
        SELECT id, user_id, channel_id, tg_msg_id,
               COALESCE(tg_doc_id, 0), COALESCE(access_hash, 0), file_reference,
               COALESCE(mime_type, ''), COALESCE(file_size, 0), COALESCE(dc_id, 0)
        FROM videos WHERE id=$1
    `, videoID)
	v := &db.Video{}
	if err := row.Scan(&v.ID, &v.UserID, &v.ChannelID, &v.TGMsgID,
		&v.TGDocID, &v.AccessHash, &v.FileReference,
		&v.MimeType, &v.FileSize, &v.DCID); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *Manager) gcLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	m.evictIfNeeded(ctx)
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			m.evictIfNeeded(ctx)
		}
	}
}

// evictIfNeeded reconciles the database inventory with the actual video
// directory, removes stale files, then enforces the configured cap using real
// on-disk bytes. DB accounting alone cannot see files left by a crash between
// rename and DB upsert.
func (m *Manager) evictIfNeeded(ctx context.Context) {
	cap := m.cfg.CacheCapBytes()
	if cap <= 0 {
		return
	}
	files, total, err := m.scanCacheFiles()
	if err != nil {
		slog.Warn("scan cache directory", "err", err)
		return
	}
	// Thumbnails are not LRU-managed (they are tiny and re-fetching one on every
	// grid scroll would be worse than keeping it), but they do occupy the same
	// disk, so their bytes count against the cap.
	thumbBytes := dirBytes(m.ThumbDir())
	entries, err := m.db.AllCompletedCacheEntries(ctx)
	if err != nil {
		slog.Warn("load cache inventory", "err", err)
		return
	}
	byKey := make(map[cacheKey]db.CacheEntry, len(entries))
	for _, e := range entries {
		byKey[cacheKey{kind: e.Kind, id: e.TGDocID}] = e
	}
	var orphanBytes int64
	for _, f := range files {
		if _, ok := byKey[f.Key]; ok {
			continue
		}
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("remove orphan cache file", "path", f.Path, "err", err)
			continue
		}
		total -= f.Bytes
		orphanBytes += f.Bytes
		slog.Info("orphan cache removed", "kind", f.Key.kind, "doc_id", f.Key.id, "bytes", f.Bytes)
	}
	if orphanBytes > 0 {
		slog.Warn("cache reconciliation removed orphan files", "bytes", orphanBytes)
	}
	fileByKey := make(map[cacheKey]diskFile, len(files))
	for _, f := range files {
		fileByKey[f.Key] = f
	}
	for _, e := range entries {
		k := cacheKey{kind: e.Kind, id: e.TGDocID}
		if _, ok := fileByKey[k]; !ok {
			if err := m.db.DeleteCacheEntry(ctx, e.Kind, e.TGDocID); err != nil {
				slog.Warn("delete stale cache record", "kind", e.Kind, "doc_id", e.TGDocID, "err", err)
			}
		}
	}
	candidates := make([]db.CacheEntry, 0, len(entries))
	for _, e := range entries {
		if f, ok := fileByKey[cacheKey{kind: e.Kind, id: e.TGDocID}]; ok {
			e.Bytes = f.Bytes
			candidates = append(candidates, e)
		}
	}
	total += thumbBytes
	if total <= cap {
		slog.Info("cache capacity check", "disk_bytes", total, "thumb_bytes", thumbBytes, "cap_bytes", cap, "evicted_bytes", int64(0))
		return
	}
	// Unpinned caches go first. If pinned favorites alone exceed the cap, strict
	// disk protection wins and least-recently-used pinned files are reclaimed too.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Pinned != candidates[j].Pinned {
			return !candidates[i].Pinned
		}
		return candidates[i].LastAccessedAt.Before(candidates[j].LastAccessedAt)
	})
	var evicted, evictedBytes int64
	for _, e := range candidates {
		if total <= cap {
			break
		}
		f, ok := fileByKey[cacheKey{kind: e.Kind, id: e.TGDocID}]
		if !ok {
			continue
		}
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("evict cache file", "path", f.Path, "err", err)
			continue
		}
		if err := m.db.DeleteCacheEntry(ctx, e.Kind, e.TGDocID); err != nil {
			slog.Warn("delete evicted cache record", "kind", e.Kind, "doc_id", e.TGDocID, "err", err)
		}
		total -= f.Bytes
		evictedBytes += f.Bytes
		evicted++
		slog.Info("cache evicted", "kind", e.Kind, "doc_id", e.TGDocID, "bytes", f.Bytes, "pinned", e.Pinned)
	}
	slog.Info("cache capacity check", "disk_bytes", total, "cap_bytes", cap, "evicted_files", evicted, "evicted_bytes", evictedBytes)
	if total > cap {
		slog.Error("cache remains above cap after eviction", "disk_bytes", total, "cap_bytes", cap)
	}
}

// scanCacheFiles walks both media directories. The disk — not the DB — is the
// source of truth for usage, so this is what the cap is enforced against.
func (m *Manager) scanCacheFiles() ([]diskFile, int64, error) {
	var (
		all   []diskFile
		total int64
	)
	for _, d := range []struct {
		kind string
		dir  string
	}{
		{db.MediaKindVideo, m.videoDir()},
		{db.MediaKindPhoto, m.photoDir()},
	} {
		files, sum, err := scanCacheDir(d.kind, d.dir)
		if err != nil {
			return nil, 0, err
		}
		all = append(all, files...)
		total += sum
	}
	return all, total, nil
}

func scanCacheDir(kind, dir string) ([]diskFile, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	files := make([]diskFile, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".bin") {
			continue
		}
		docID, err := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".bin"), 10, 64)
		if err != nil || docID <= 0 {
			slog.Warn("ignore invalid cache filename", "name", entry.Name())
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, 0, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		f := diskFile{
			Key:   cacheKey{kind: kind, id: docID},
			Path:  filepath.Join(dir, entry.Name()),
			Bytes: info.Size(),
		}
		files = append(files, f)
		total += f.Bytes
	}
	return files, total, nil
}

// dirBytes sums the regular files directly inside dir (non-recursive). Missing
// directory = 0 bytes.
func dirBytes(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
	}
	return total
}

func (m *Manager) cleanPartials() error {
	for _, kind := range []string{db.MediaKindVideo, db.MediaKindPhoto} {
		dir := m.tmpDir(kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		for _, e := range entries {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}
func relPath(parts ...string) string { return filepath.Join(parts...) }
