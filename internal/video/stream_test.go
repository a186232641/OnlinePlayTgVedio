package video

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// synthByte is the deterministic content of a fake file at absolute position p.
func synthByte(p int64) byte { return byte(p * 1103515245 % 251) }

// fakeFetch serves aligned chunkSize blocks of a synthetic file of fileSize
// bytes. delay introduces per-block latency so blocks complete out of order,
// exercising streamWindowed's reordering. If failAt >= 0, the block containing
// that offset returns an error.
func fakeFetch(fileSize int64, delays []time.Duration, failAt int64) func(context.Context, int64) ([]byte, error) {
	return func(ctx context.Context, offset int64) ([]byte, error) {
		if d := delays[int(offset/chunkSize)%len(delays)]; d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if failAt >= 0 && offset <= failAt && failAt < offset+chunkSize {
			return nil, errors.New("boom")
		}
		if offset >= fileSize {
			return []byte{}, nil
		}
		n := int64(chunkSize)
		if offset+n > fileSize {
			n = fileSize - offset
		}
		b := make([]byte, n)
		for i := range b {
			b[i] = synthByte(offset + int64(i))
		}
		return b, nil
	}
}

func wantSynth(start, end int64) []byte {
	b := make([]byte, end-start+1)
	for i := range b {
		b[i] = synthByte(start + int64(i))
	}
	return b
}

func TestStreamWindowed(t *testing.T) {
	const fileSize = 10*chunkSize + 12345 // ~10 MiB, last block partial
	delays := []time.Duration{3 * time.Millisecond, 0, 1 * time.Millisecond, 2 * time.Millisecond}

	cases := []struct{ name string; start, end int64 }{
		{"from-zero", 0, fileSize - 1},
		{"unaligned-start", chunkSize + 777, 5*chunkSize + 50},
		{"single-byte", 4*chunkSize + 1, 4*chunkSize + 1},
		{"within-one-block", 100, 200},
		{"tail", fileSize - 5000, fileSize - 1},
		{"sub-block-spanning-boundary", chunkSize - 10, chunkSize + 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			n, err := streamWindowed(context.Background(), fakeFetch(fileSize, delays, -1), c.start, c.end, &buf, 1)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			want := wantSynth(c.start, c.end)
			if n != int64(len(want)) {
				t.Fatalf("written=%d want=%d", n, len(want))
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Fatalf("content mismatch for [%d,%d]", c.start, c.end)
			}
		})
	}
}

// TestStreamWindowedEOF: requesting past the real EOF stops at the file end
// instead of hanging, returning only the bytes that exist.
func TestStreamWindowedEOF(t *testing.T) {
	const fileSize = 3*chunkSize + 500
	var buf bytes.Buffer
	// Ask for more than the file holds (caller's end overshoots).
	n, err := streamWindowed(context.Background(), fakeFetch(fileSize, []time.Duration{0}, -1), 0, 8*chunkSize, &buf, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != fileSize {
		t.Fatalf("written=%d want=%d", n, fileSize)
	}
	if !bytes.Equal(buf.Bytes(), wantSynth(0, fileSize-1)) {
		t.Fatal("content mismatch")
	}
}

// TestStreamWindowedError: a fetch error is returned and partial bytes written
// so far are reported (so the caller can resume).
func TestStreamWindowedError(t *testing.T) {
	const fileSize = 10 * chunkSize
	var buf bytes.Buffer
	// Fail the block at offset 5 MiB; blocks 0-4 should still be written.
	n, err := streamWindowed(context.Background(), fakeFetch(fileSize, []time.Duration{0}, 5*chunkSize), 0, fileSize-1, &buf, 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if n != 5*chunkSize {
		t.Fatalf("written=%d want=%d", n, 5*chunkSize)
	}
	if !bytes.Equal(buf.Bytes(), wantSynth(0, 5*chunkSize-1)) {
		t.Fatal("prefix content mismatch")
	}
}

func TestParseRange(t *testing.T) {
	const size int64 = 10000
	cases := []struct {
		header     string
		wantStart  int64
		wantEnd    int64
		wantOK     bool
	}{
		{"", 0, 9999, true},
		{"bytes=0-", 0, 9999, true},
		{"bytes=0-499", 0, 499, true},
		{"bytes=500-999", 500, 999, true},
		{"bytes=-500", 9500, 9999, true},
		{"bytes=9500-", 9500, 9999, true},
		{"bytes=9999-", 9999, 9999, true},
		{"bytes=10000-", 0, 0, false}, // start past EOF
		{"bytes=500-100", 0, 0, false}, // end < start
		{"items=0-499", 0, 0, false},
		{"bytes=", 0, 0, false},
	}
	for _, c := range cases {
		s, e, ok := parseRange(c.header, size)
		if ok != c.wantOK {
			t.Errorf("%q ok=%v want %v", c.header, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if s != c.wantStart || e != c.wantEnd {
			t.Errorf("%q got [%d,%d] want [%d,%d]", c.header, s, e, c.wantStart, c.wantEnd)
		}
	}
}
