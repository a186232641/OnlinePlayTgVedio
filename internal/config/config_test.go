package config

import (
	"testing"
	"time"
)

func TestParseDCOverrides(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := parseDCOverrides("")
		if err != nil || got != nil {
			t.Fatalf("got %v, %v", got, err)
		}
	})

	t.Run("single default port", func(t *testing.T) {
		got, err := parseDCOverrides("5=91.108.56.100")
		if err != nil {
			t.Fatal(err)
		}
		want := []DCOverride{{ID: 5, IP: "91.108.56.100", Port: 443}}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})

	t.Run("multiple with ports and spaces", func(t *testing.T) {
		got, err := parseDCOverrides(" 5=91.108.56.100:443 , 4=149.154.167.91:8443 ")
		if err != nil {
			t.Fatal(err)
		}
		want := []DCOverride{
			{ID: 5, IP: "91.108.56.100", Port: 443},
			{ID: 4, IP: "149.154.167.91", Port: 8443},
		}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})

	t.Run("ipv6 bracketed with port", func(t *testing.T) {
		got, err := parseDCOverrides("2=[2001:67c:4e8:f002::a]:443")
		if err != nil {
			t.Fatal(err)
		}
		want := DCOverride{ID: 2, IP: "2001:67c:4e8:f002::a", Port: 443}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})

	t.Run("ipv6 bare no port", func(t *testing.T) {
		got, err := parseDCOverrides("2=2001:67c:4e8:f002::a")
		if err != nil {
			t.Fatal(err)
		}
		want := DCOverride{ID: 2, IP: "2001:67c:4e8:f002::a", Port: 443}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("got %+v want %+v", got, want)
		}
	})

	for _, bad := range []string{"5", "=1.2.3.4", "x=1.2.3.4", "5=1.2.3.4:bad", "5="} {
		t.Run("bad/"+bad, func(t *testing.T) {
			if _, err := parseDCOverrides(bad); err == nil {
				t.Fatalf("expected error for %q", bad)
			}
		})
	}
}
func TestSyncRunTimeoutParsing(t *testing.T) {
	// Same parser as SYNC_INTERVAL, but 0/off means "no overall deadline" rather
	// than "disabled" — a run is then bounded only by the per-page timeout.
	cases := map[string]time.Duration{
		"24h": 24 * time.Hour,
		"90m": 90 * time.Minute,
		"0":   0,
		"off": 0,
		"":    0,
	}
	for in, want := range cases {
		got, err := parseSyncInterval(in)
		if err != nil || got != want {
			t.Errorf("parseSyncInterval(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseSyncInterval("nonsense"); err == nil {
		t.Error("parseSyncInterval(nonsense) should fail rather than silently disable the limit")
	}
}
