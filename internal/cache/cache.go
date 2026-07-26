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

type diskFile struct {
	DocID int64
	Path  string
	Bytes int64
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

func (m *Manager) Stop()            { m.stopOnce.Do(func() { close(m.stopCh) }) }
func (m *Manager) videoDir() string { return filepath.Join(m.cfg.CacheDir, "videos") }
func (m *Manager) tmpDir() string   { return filepath.Join(m.cfg.CacheDir, "videos", "tmp") }
func (m *Manager) videoPath(docID int64) string {
	return filepath.Join(m.videoDir(), fmt.Sprintf("%d.bin", docID))
}

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

func (m *Manager) Touch(ctx context.Context, docID int64) { _ = m.db.TouchCache(ctx, docID) }

// InvalidateCorrupt drops a cached file that failed a serve-time integrity check
// (wrong on-disk size) and resets its entry so the next play re-downloads it.
// The pinned flag is preserved so favorites stay pinned across the re-download.
func (m *Manager) InvalidateCorrupt(ctx context.Context, docID int64) {
	if m == nil || docID == 0 {
		return
	}
	if err := os.Remove(m.videoPath(docID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("invalidate corrupt cache: remove file", "doc_id", docID, "err", err)
	}
	if err := m.db.MarkCacheIncomplete(ctx, docID); err != nil {
		slog.Warn("invalidate corrupt cache: mark incomplete", "doc_id", docID, "err", err)
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
	files, total, err := scanVideoFiles(m.videoDir())
	if err != nil {
		slog.Warn("scan cache directory", "err", err)
		return
	}
	entries, err := m.db.AllCompletedCacheEntries(ctx)
	if err != nil {
		slog.Warn("load cache inventory", "err", err)
		return
	}
	byDoc := make(map[int64]db.CacheEntry, len(entries))
	for _, e := range entries {
		byDoc[e.TGDocID] = e
	}
	var orphanBytes int64
	for _, f := range files {
		if _, ok := byDoc[f.DocID]; ok {
			continue
		}
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("remove orphan cache file", "path", f.Path, "err", err)
			continue
		}
		total -= f.Bytes
		orphanBytes += f.Bytes
		slog.Info("orphan cache removed", "doc_id", f.DocID, "bytes", f.Bytes)
	}
	if orphanBytes > 0 {
		slog.Warn("cache reconciliation removed orphan files", "bytes", orphanBytes)
	}
	present := make(map[int64]struct{}, len(files))
	fileByDoc := make(map[int64]diskFile, len(files))
	for _, f := range files {
		present[f.DocID] = struct{}{}
		fileByDoc[f.DocID] = f
	}
	for _, e := range entries {
		if _, ok := present[e.TGDocID]; !ok {
			if err := m.db.DeleteCacheEntry(ctx, e.TGDocID); err != nil {
				slog.Warn("delete stale cache record", "doc_id", e.TGDocID, "err", err)
			}
		}
	}
	candidates := make([]db.CacheEntry, 0, len(entries))
	for _, e := range entries {
		if f, ok := fileByDoc[e.TGDocID]; ok {
			e.Bytes = f.Bytes
			candidates = append(candidates, e)
		}
	}
	if total <= cap {
		slog.Info("cache capacity check", "disk_bytes", total, "cap_bytes", cap, "evicted_bytes", int64(0))
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
		f, ok := fileByDoc[e.TGDocID]
		if !ok {
			continue
		}
		if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("evict cache file", "path", f.Path, "err", err)
			continue
		}
		if err := m.db.DeleteCacheEntry(ctx, e.TGDocID); err != nil {
			slog.Warn("delete evicted cache record", "doc_id", e.TGDocID, "err", err)
		}
		total -= f.Bytes
		evictedBytes += f.Bytes
		evicted++
		slog.Info("cache evicted", "doc_id", e.TGDocID, "bytes", f.Bytes, "pinned", e.Pinned)
	}
	slog.Info("cache capacity check", "disk_bytes", total, "cap_bytes", cap, "evicted_files", evicted, "evicted_bytes", evictedBytes)
	if total > cap {
		slog.Error("cache remains above cap after eviction", "disk_bytes", total, "cap_bytes", cap)
	}
}

func scanVideoFiles(dir string) ([]diskFile, int64, error) {
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
		f := diskFile{DocID: docID, Path: filepath.Join(dir, entry.Name()), Bytes: info.Size()}
		files = append(files, f)
		total += f.Bytes
	}
	return files, total, nil
}

func (m *Manager) cleanPartials() error {
	entries, err := os.ReadDir(m.tmpDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(m.tmpDir(), e.Name()))
	}
	return nil
}
func relPath(parts ...string) string { return filepath.Join(parts...) }
