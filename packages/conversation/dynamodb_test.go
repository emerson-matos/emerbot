package conversation

import (
	"testing"
	"time"
)

// TestSortKeyOrdersChronologically proves later append times yield
// lexicographically larger keys, so a newest-first Query returns turns in true
// arrival order regardless of the string comparison DynamoDB uses.
func TestSortKeyOrdersChronologically(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	earlier := sortKey(base)
	later := sortKey(base.Add(time.Nanosecond))

	if earlier >= later {
		t.Fatalf("expected earlier key %q < later key %q", earlier, later)
	}
}

// TestSortKeyUniqueWithinSameInstant proves two turns stamped at the exact same
// time still get distinct keys, so one can never overwrite the other on PutItem.
func TestSortKeyUniqueWithinSameInstant(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	seen := make(map[string]struct{}, 1000)
	for range 1000 {
		k := sortKey(now)
		if _, dup := seen[k]; dup {
			t.Fatalf("duplicate sort key generated for the same instant: %q", k)
		}
		seen[k] = struct{}{}
	}
}
