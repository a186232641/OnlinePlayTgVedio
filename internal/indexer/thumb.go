package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
)

// fetchThumb selects the largest available thumbnail for the document and
// stores it under <CACHE_DIR>/thumbs/<doc_id>.jpg. Returns the relative path
// (e.g. "thumbs/123.jpg") or empty + nil if no thumb is available.
func (i *Indexer) fetchThumb(ctx context.Context, dl *downloader.Downloader, raw downloader.Client, doc *tg.Document) (string, error) {
	if len(doc.Thumbs) == 0 {
		return "", nil
	}

	// Prefer a sized thumb; pick the largest by reported dimensions.
	var best *tg.PhotoSize
	for _, t := range doc.Thumbs {
		if ps, ok := t.(*tg.PhotoSize); ok {
			if best == nil || ps.W*ps.H > best.W*best.H {
				best = ps
			}
		}
	}
	if best == nil {
		return "", nil
	}

	relDir := "thumbs"
	absDir := filepath.Join(i.cfg.CacheDir, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir thumbs: %w", err)
	}
	relPath := filepath.Join(relDir, fmt.Sprintf("%d.jpg", doc.ID))
	absPath := filepath.Join(i.cfg.CacheDir, relPath)
	if _, err := os.Stat(absPath); err == nil {
		return relPath, nil
	}

	loc := &tg.InputDocumentFileLocation{
		ID:            doc.ID,
		AccessHash:    doc.AccessHash,
		FileReference: doc.FileReference,
		ThumbSize:     best.Type,
	}
	if _, err := dl.Download(raw, loc).ToPath(ctx, absPath); err != nil {
		_ = os.Remove(absPath)
		return "", err
	}
	return relPath, nil
}
