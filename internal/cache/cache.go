package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
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

type diskFile struct {
	DocID int64
	Path  string
	Bytes int64
}

func New(cfg *config.Config, database *db.DB, mgr *tgmanager.Manager) *Manager {
	return &Manager{cfg: cfg, db: database, tg: mgr, queue: make(chan int64, 256), queued: map[int64]struct{}{}, stopCh: make(chan struct{})}
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

func (m *Manager) MaybeTee(ctx context.Context, v *db.Video, start int64) (io.Writer, func(), bool) {
	if start != 0 {
		return nil, nil, false
	}
	c, err := m.db.GetCacheEntry(ctx, v.TGDocID)
	if err == nil && c.Completed {
		return nil, nil, false
	}
	if err == nil && !c.Pinned {
		return nil, nil, false
	}
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, nil, false
	}
	if err == db.ErrNotFound {
		return nil, nil, false
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
		if st.Size() != v.FileSize {
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
		_ = m.db.UpsertCacheEntry(context.Background(), &db.CacheEntry{TGDocID: v.TGDocID, FilePath: rel, Bytes: st.Size(), Pinned: true, Completed: true})
	}
	return f, finish, true
}

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
	_ = m.db.UpsertCacheEntry(ctx, &db.CacheEntry{TGDocID: docID, FilePath: relPath("videos", fmt.Sprintf("%d.bin", docID)), Pinned: true})
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
	}
}

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
	if c, err := m.db.GetCacheEntry(ctx, v.TGDocID); err == nil && c.Completed {
		return nil
	}
	if err := os.MkdirAll(m.tmpDir(), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(m.tmpDir(), fmt.Sprintf("%d.dl", v.TGDocID))
	final := m.videoPath(v.TGDocID)
	dl := downloader.NewDownloader().WithPartSize(512 * 1024)
	loc := &tg.InputDocumentFileLocation{ID: v.TGDocID, AccessHash: v.AccessHash, FileReference: v.FileReference}
	if _, err := dl.Download(cli.API, loc).ToPath(ctx, tmp); err != nil {
		_ = os.Remove(tmp)
		if tgerr.Is(err, "FILE_REFERENCE_EXPIRED") {
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
	return m.db.UpsertCacheEntry(ctx, &db.CacheEntry{TGDocID: v.TGDocID, FilePath: rel, Bytes: st.Size(), Pinned: true, Completed: true})
}

func (m *Manager) lookupVideoForDownload(ctx context.Context, videoID int64) (*db.Video, error) {
	row := m.db.QueryRow(ctx, `SELECT id, user_id, channel_id, tg_msg_id, COALESCE(tg_doc_id, 0), COALESCE(access_hash, 0), file_reference, COALESCE(mime_type, ''), COALESCE(file_size, 0) FROM videos WHERE id=$1`, videoID)
	v := &db.Video{}
	if err := row.Scan(&v.ID, &v.UserID, &v.ChannelID, &v.TGMsgID, &v.TGDocID, &v.AccessHash, &v.FileReference, &v.MimeType, &v.FileSize); err != nil {
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
