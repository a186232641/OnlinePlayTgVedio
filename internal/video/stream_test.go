package video

import "testing"

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
