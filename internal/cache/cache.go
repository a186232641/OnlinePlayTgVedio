package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// Manager owns the on-disk cache directory, the background download worker
// for favorites, and the periodic LRU eviction job.
type Manager struct {
	cfg *config.Config
	db  *db.DB
	tg  *tgmanager.Manager

	queue chan int64

	mu       sync.Mutex
	queued   map[int64]struct{}
	stopOnce sync.Once
	stopCh   chan struct{}
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Manager {
	return &Manager{
		cfg:    cfg,
		db:     database,
		tg:     mgr,
		queue:  make(chan int64, 256),
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

// MaybeTee returns a writer that mirrors the ongoing stream into a partial
// cache file when (a) the video is favorited, (b) the byte range starts at
// offset 0, and (c) no complete cache file exists yet. Otherwise ok=false.
func (m *Manager) MaybeTee(ctx context.Context, v *db.Video, start int64) (io.Writer, func(), bool) {
	if start != 0 {
		return nil, nil, false
	}
	c, err := m.db.GetCacheEntry(ctx, v.TGDocID)
	if err == nil && c.Completed {
		return nil, nil, false
	}
	// Pinned (i.e. favorite) mirrors. Non-pinned: skip mirroring (already
	// causes more disk IO than it's worth — eviction would just delete it).
	if err == nil && !c.Pinned {
		return nil, nil, false
	}
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, nil, false
	}
	if err == db.ErrNotFound {
		return nil, nil, false // not even queued — let the worker handle it
	}
	if err := os.MkdirAll(m.tmpDir(), 0o755); err != nil {
		return nil, nil, false
	}
	tmpPath := filepath.Join(m.tmpDir(), fmt.Sprintf("%d.part", v.TGDocID))
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, nil, false
	}
	finish := func() {
		_ = f.Close()
		st, err := os.Stat(tmpPath)
		if err != nil {
			return
		}
		// Only promote a fully-finished tee — but we don't know full size in
		// this path. Treat as complete only when written bytes equal the
		// recorded video size.
		if st.Size() != v.SizeBytes {
			_ = os.Remove(tmpPath)
			return
		}
		final := m.videoPath(v.TGDocID)
		if err := os.Rename(tmpPath, final); err != nil {
			slog.Warn("promote cache tee", "err", err)
			_ = os.Remove(tmpPath)
			return
		}
		rel, _ := filepath.Rel(m.cfg.CacheDir, final)
		_ = m.db.UpsertCacheEntry(context.Background(), &db.CacheEntry{
			TGDocID:   v.TGDocID,
			FilePath:  rel,
			Bytes:     st.Size(),
			Pinned:    true,
			Completed: true,
		})
	}
	return f, finish, true
}

// EnqueueFavorite is called when a user adds a favorite. It marks the doc
// pinned and (if no complete file exists yet) schedules a background download.
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
	// Make sure a placeholder exists so future MaybeTee can run.
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		TGDocID:  docID,
		FilePath: relPath("videos", fmt.Sprintf("%d.bin", docID)),
		Pinned:   true,
	})

	m.mu.Lock()
	if _, ok := m.queued[videoID]; ok {
		m.mu.Unlock()
		return
	}
	m.queued[videoID] = struct{}{}
	m.mu.Unlock()

	select {
	case m.queue <- videoID:
	default:
		// queue is full; drop request — gcLoop will retry pinned not-completed
	}
}

// HandleUnfavorite is called when a favorite is removed. It unpins the cache
// entry only if no other user still has it favorited.
func (m *Manager) HandleUnfavorite(userID, videoID int64) {
	_ = m.db.UnpinIfNotFavorited(context.Background(), videoID)
}

func (m *Manager) workerLoop(ctx context.Context) {
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case vid := <-m.queue:
			if err := m.downloadFavorite(ctx, vid); err != nil {
				slog.Warn("favorite download failed", "video_id", vid, "err", err)
			}
			m.mu.Lock()
			delete(m.queued, vid)
			m.mu.Unlock()
		}
	}
}

func (m *Manager) downloadFavorite(ctx context.Context, videoID int64) error {
	v, err := m.lookupVideoForDownload(ctx, videoID)
	if err != nil {
		return err
	}
	ch, err := m.db.ChannelByID(ctx, v.ChannelID, v.UserID)
	if err != nil {
		return err
	}
	cli, err := m.tg.ClientForSession(ch.TGSessionID)
	if err != nil {
		return err
	}

	// Skip if a fresh complete file already exists.
	if c, err := m.db.GetCacheEntry(ctx, v.TGDocID); err == nil && c.Completed {
		return nil
	}

	if err := os.MkdirAll(m.tmpDir(), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(m.tmpDir(), fmt.Sprintf("%d.dl", v.TGDocID))
	final := m.videoPath(v.TGDocID)

	dl := downloader.NewDownloader().WithPartSize(512 * 1024)
	loc := &tg.InputDocumentFileLocation{
		ID:            v.TGDocID,
		AccessHash:    v.AccessHash,
		FileReference: v.FileReference,
	}
	if _, err := dl.Download(cli.API, loc).ToPath(ctx, tmp); err != nil {
		_ = os.Remove(tmp)
		if tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
			// caller will retry next cycle; we don't try to refresh here.
			return fmt.Errorf("file_reference expired (will retry): %w", err)
		}
		return err
	}
	st, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		return err
	}
	rel, _ := filepath.Rel(m.cfg.CacheDir, final)
	return m.db.UpsertCacheEntry(ctx, &db.CacheEntry{
		TGDocID:   v.TGDocID,
		FilePath:  rel,
		Bytes:     st.Size(),
		Pinned:    true,
		Completed: true,
	})
}

func (m *Manager) lookupVideoForDownload(ctx context.Context, videoID int64) (*db.Video, error) {
	row := m.db.QueryRow(ctx, `
        SELECT id, user_id, channel_id, tg_message_id, tg_doc_id, access_hash, file_reference, COALESCE(mime,''), size_bytes
        FROM videos WHERE id=$1
    `, videoID)
	v := &db.Video{}
	if err := row.Scan(&v.ID, &v.UserID, &v.ChannelID, &v.TGMessageID, &v.TGDocID, &v.AccessHash, &v.FileReference, &v.Mime, &v.SizeBytes); err != nil {
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
