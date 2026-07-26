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

	// Ascending date flips both comparisons to ">" so paging walks oldest→newest.
	asc := keysetCursor("date_asc", "2")
	for _, want := range []string{"v.date > ", "v.id > $2"} {
		if !strings.Contains(asc, want) {
			t.Fatalf("date_asc cursor missing %q in: %s", want, asc)
		}
	}
	if strings.Contains(asc, "v.date < ") {
		t.Fatalf("date_asc cursor must not contain '<': %s", asc)
	}

	// Name ordering keysets on file_name, not date.
	nameAsc := keysetCursor("name_asc", "5")
	for _, want := range []string{"SELECT file_name FROM videos WHERE id = $5", "v.file_name > ", "v.id > $5"} {
		if !strings.Contains(nameAsc, want) {
			t.Fatalf("name_asc cursor missing %q in: %s", want, nameAsc)
		}
	}
	nameDesc := keysetCursor("name_desc", "5")
	if !strings.Contains(nameDesc, "v.file_name < ") {
		t.Fatalf("name_desc cursor must contain '<': %s", nameDesc)
	}
}

func TestOrderClause(t *testing.T) {
	cases := map[string]string{
		"":          "ORDER BY v.date DESC NULLS LAST, v.id DESC",
		"date_desc": "ORDER BY v.date DESC NULLS LAST, v.id DESC",
		"date_asc":  "ORDER BY v.date ASC NULLS LAST, v.id ASC",
		"name_asc":  "ORDER BY v.file_name ASC NULLS LAST, v.id ASC",
		"name_desc": "ORDER BY v.file_name DESC NULLS LAST, v.id DESC",
		"duration":  "ORDER BY v.duration_seconds DESC, v.id DESC",
		"garbage":   "ORDER BY v.date DESC NULLS LAST, v.id DESC", // unknown → default
	}
	for in, want := range cases {
		if got := orderClause(in); !strings.Contains(got, want) {
			t.Errorf("orderClause(%q) = %q, want contains %q", in, got, want)
		}
	}
}