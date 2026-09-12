package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanCacheDir(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n int) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("100.bin", 3)
	write("200.bin", 7)
	write("ignored.txt", 9)
	write("invalid.bin", 11)
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, total, err := scanCacheDir("video", dir)
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 || len(files) != 2 {
		t.Fatalf("files=%d total=%d; want files=2 total=10", len(files), total)
	}
	got := map[int64]int64{}
	for _, f := range files {
		got[f.Key.id] = f.Bytes
	}
	if got[100] != 3 || got[200] != 7 {
		t.Fatalf("unexpected files: %#v", got)
	}
}

func TestScanCacheDirMissingDirectory(t *testing.T) {
	files, total, err := scanCacheDir("video", filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || total != 0 {
		t.Fatalf("files=%d total=%d; want empty", len(files), total)
	}
}

func TestThumbCap(t *testing.T) {
	const gb = int64(1) << 30
	// 10% of the overall cap…
	if got := thumbCap(100 * gb); got != 10*gb {
		t.Fatalf("thumbCap(100GiB) = %d, want %d", got, 10*gb)
	}
	// …but never below the floor, so a small configured cap still leaves room
	// for the grid to be usable.
	if got := thumbCap(4 * gb); got != thumbCapFloor {
		t.Fatalf("thumbCap(4GiB) = %d, want the floor %d", got, thumbCapFloor)
	}
	// …and never more than the whole cap.
	if got := thumbCap(gb / 2); got != gb/2 {
		t.Fatalf("thumbCap(512MiB) = %d, want %d", got, gb/2)
	}
}

func TestScanThumbFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, n int) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("video_12.jpg", 3)
	write("photo_34.jpg", 7)
	write("readme.txt", 100)    // not a thumb
	write("garbage.jpg", 100)   // no kind_id shape
	write("other_5.jpg", 100)   // unknown kind
	write("photo_abc.jpg", 100) // unparseable id

	files, total := scanThumbFiles(dir)
	if total != 10 || len(files) != 2 {
		t.Fatalf("files=%d total=%d; want 2/10 (foreign files must be ignored, not reclaimed)", len(files), total)
	}
	got := map[string]int64{}
	for _, f := range files {
		got[f.Kind] = f.RowID
	}
	if got["video"] != 12 || got["photo"] != 34 {
		t.Fatalf("parsed ids wrong: %#v", got)
	}
}

func TestScanThumbFilesMissingDirectory(t *testing.T) {
	files, total := scanThumbFiles(filepath.Join(t.TempDir(), "nope"))
	if len(files) != 0 || total != 0 {
		t.Fatalf("files=%d total=%d; want empty", len(files), total)
	}
}
