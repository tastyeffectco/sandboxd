package api

import "testing"

// isULID guards destructive/path-deriving handlers (purge, file write).
func TestIsULIDRejectsUnsafeIDs(t *testing.T) {
	for _, id := range []string{"", "..", "../etc", "a/b", `a\b`, "/abs", "short", "0"} {
		if isULID(id) {
			t.Errorf("isULID(%q) = true, want false", id)
		}
	}
	if !isULID("01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Error("isULID rejected a valid 26-char ULID")
	}
}
