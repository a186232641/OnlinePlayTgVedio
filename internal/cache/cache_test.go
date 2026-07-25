package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanVideoFiles(t *testing.T) {
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

	files, total, err := scanVideoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 || len(files) != 2 {
		t.Fatalf("files=%d total=%d; want files=2 total=10", len(files), total)
	}
	got := map[int64]int64{}
	for _, f := range files {
		got[f.DocID] = f.Bytes
	}
	if got[100] != 3 || got[200] != 7 {
		t.Fatalf("unexpected files: %#v", got)
	}
}

func TestScanVideoFilesMissingDirectory(t *testing.T) {
	files, total, err := scanVideoFiles(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 || total != 0 {
		t.Fatalf("files=%d total=%d; want empty", len(files), total)
	}
}
