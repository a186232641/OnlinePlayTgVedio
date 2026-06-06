package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// cacheJob is one queued download. docID lets the worker dedup and release the
// in-flight slot even if the per-video lookup later fails.
type cacheJob struct {
	videoID int64
	docID   int64
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

	queue chan cacheJob

	mu       sync.Mutex
	queued   map[int64]struct{} // docIDs currently queued/in-flight
	stopOnce sync.Once
	stopCh   chan struct{}
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Manager {
	return &Manager{
		cfg:    cfg,
		db:     database,
		tg:     mgr,
		queue:  make(chan cacheJob, 256),
		queued: map[int64]struct{}{},
		stopCh: make(chan struct{}),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	if err := os.MkdirAll(m.videoDir(), 0o755); err != nil {
		return err
	}
	if err := m.cleanPartials(); err != nil {
		slog.Warn("clean partial caches", "err", err)
	}
	go m.workerLoop(ctx)
	go m.gcLoop(ctx)
	return nil
}

func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
}

func (m *Manager) videoDir() string { return filepath.Join(m.cfg.CacheDir, "videos") }
func (m *Manager) tmpDir() string   { return filepath.Join(m.cfg.CacheDir, "videos", "tmp") }

func (m *Manager) videoPath(docID int64) string {
	return filepath.Join(m.videoDir(), fmt.Sprintf("%d.bin", docID))
}

// CompletePathFor returns the absolute path of a fully cached file for docID,
// or ok=false if not present.
func (m *Manager) CompletePathFor(ctx context.Context, docID int64) (string, bool) {
	c, err := m.db.GetCacheEntry(ctx, docID)
	if err != nil || !c.Completed {
		return "", false
	}
	abs := filepath.Join(m.cfg.CacheDir, c.FilePath)
	if _, err := os.Stat(abs); err != nil {
		return "", false
	}
	return abs, true
}

func (m *Manager) Touch(ctx context.Context, docID int64) {
	_ = m.db.TouchCache(ctx, docID)
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
	if c, err := m.db.GetCacheEntry(ctx, docID); err == nil && c.Completed {
		return
	}
	// Placeholder so the entry exists; bytes=0/completed=false keeps it out of
	// the total and LRU until the download finishes. Upsert's OR semantics never
	// un-pin an already-favorited entry.
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		TGDocID:  docID,
		FilePath: relPath("videos", fmt.Sprintf("%d.bin", docID)),
		Pinned:   false,
	})
	m.submit(videoID, docID)
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
	if completed {
		return
	}
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		TGDocID:  docID,
		FilePath: relPath("videos", fmt.Sprintf("%d.bin", docID)),
		Pinned:   true,
	})
	m.submit(videoID, docID)
}

// HandleUnfavorite is called when a favorite is removed. It unpins the cache
// entry only if no other user still has it favorited.
func (m *Manager) HandleUnfavorite(userID, videoID int64) {
	_ = m.db.UnpinIfNotFavorited(context.Background(), videoID)
}

// submit enqueues a download job, deduped by docID. A full queue drops the
// request (a later play or GC pass re-submits it).
func (m *Manager) submit(videoID, docID int64) {
	if docID == 0 {
		return
	}
	m.mu.Lock()
	if _, ok := m.queued[docID]; ok {
		m.mu.Unlock()
		return
	}
	m.queued[docID] = struct{}{}
	m.mu.Unlock()

	select {
	case m.queue <- cacheJob{videoID: videoID, docID: docID}:
	default:
		m.mu.Lock()
		delete(m.queued, docID)
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
			delete(m.queued, job.docID)
			m.mu.Unlock()
		}
	}
}

func (m *Manager) runDownload(ctx context.Context, job cacheJob) {
	v, err := m.lookupVideoForDownload(ctx, job.videoID)
	if err != nil {
		slog.Warn("cache lookup video", "video_id", job.videoID, "err", err)
		return
	}
	if err := m.downloadDoc(ctx, v); err != nil {
		slog.Warn("cache download failed", "video_id", job.videoID, "doc_id", v.TGDocID, "err", err)
	}
}

// downloadDoc downloads the whole document with the multi-threaded gotd
// downloader (the tdl template), atomically promotes the temp file, records the
// cache entry, and triggers eviction if the new file pushed us over the cap.
func (m *Manager) downloadDoc(ctx context.Context, v *db.Video) error {
	if v.TGDocID == 0 {
		return fmt.Errorf("video %d has no document locator", v.ID)
	}
	if c, err := m.db.GetCacheEntry(ctx, v.TGDocID); err == nil && c.Completed {
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

	if err := os.MkdirAll(m.tmpDir(), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(m.tmpDir(), fmt.Sprintf("%d.dl", v.TGDocID))
	final := m.videoPath(v.TGDocID)

	size, err := m.downloadParallel(ctx, cli.API, v, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	rel, _ := filepath.Rel(m.cfg.CacheDir, final)
	if err := m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		TGDocID:   v.TGDocID,
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
               COALESCE(mime_type, ''), COALESCE(file_size, 0)
        FROM videos WHERE id=$1
    `, videoID)
	v := &db.Video{}
	if err := row.Scan(&v.ID, &v.UserID, &v.ChannelID, &v.TGMsgID,
		&v.TGDocID, &v.AccessHash, &v.FileReference,
		&v.MimeType, &v.FileSize); err != nil {
		return nil, err
	}
	return v, nil
}

func (m *Manager) gcLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	// run once immediately
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

func (m *Manager) evictIfNeeded(ctx context.Context) {
	cap := m.cfg.CacheCapBytes()
	if cap <= 0 {
		return
	}
	total, err := m.db.TotalCacheBytes(ctx)
	if err != nil {
		slog.Warn("cache total query", "err", err)
		return
	}
	if total <= cap {
		return
	}
	cands, err := m.db.LRUCandidates(ctx, 100)
	if err != nil {
		slog.Warn("lru candidates", "err", err)
		return
	}
	for _, c := range cands {
		if total <= cap {
			break
		}
		abs := filepath.Join(m.cfg.CacheDir, c.FilePath)
		if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("evict file", "path", abs, "err", err)
			continue
		}
		_ = m.db.DeleteCacheEntry(ctx, c.TGDocID)
		total -= c.Bytes
		slog.Info("cache evicted", "doc_id", c.TGDocID, "bytes", c.Bytes)
	}
}

func (m *Manager) cleanPartials() error {
	dir := m.tmpDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	return nil
}

func relPath(parts ...string) string { return filepath.Join(parts...) }
