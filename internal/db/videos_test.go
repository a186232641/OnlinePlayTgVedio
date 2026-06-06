package db

import (
	"strings"
	"testing"
)

func TestKeysetCursor(t *testing.T) {
	// Duration ordering is not paginated by the frontend → plain id cursor.
	if got := keysetCursor("duration", "3"); got != "v.id < $3" {
		t.Fatalf("duration cursor = %q", got)
	}

	// Default (date) ordering must be a composite (date, id) keyset that derives
	// the boundary row's date from its id, and must include the NULL handling so
	// id-only ordering (which doesn't match ORDER BY date) can't stop paging
	// early. Guard the key pieces rather than the exact string.
	got := keysetCursor("", "2")
	for _, want := range []string{
		"SELECT date FROM videos WHERE id = $2",
		"v.date < ",
		"v.date = ",
		"v.id < $2",
		"v.date IS NULL",
		"CASE WHEN",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("date cursor missing %q in: %s", want, got)
		}
	}
	// Must NOT be the broken id-only form.
	if got == "v.id < $2" {
		t.Fatal("date cursor degenerated to id-only")
	}
}