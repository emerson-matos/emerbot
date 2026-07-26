package finance

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// The notifier runs entirely against these methods in the demo stack, so they
// need the same guarantees the DynamoDB store gives: prefs are per-user, and
// the delivery log dedupes on (user, key).

func TestInMemoryNotificationPrefsRoundTrip(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	prefs := domain.NotificationPrefs{
		UserID: "u1", WAEnabled: true, Phone: "5511999",
		NotifyDueToday: true, NotifyGoal: true,
	}

	if err := s.SaveNotificationPrefs(ctx, prefs); err != nil {
		t.Fatalf("save prefs: %v", err)
	}
	got, err := s.GetNotificationPrefs(ctx, "u1")
	if err != nil {
		t.Fatalf("get prefs: %v", err)
	}
	if got != prefs {
		t.Fatalf("prefs = %+v, want %+v", got, prefs)
	}

	// Saving again replaces rather than accumulating.
	prefs.WAEnabled = false
	if err := s.SaveNotificationPrefs(ctx, prefs); err != nil {
		t.Fatalf("re-save prefs: %v", err)
	}
	if got, _ = s.GetNotificationPrefs(ctx, "u1"); got.WAEnabled {
		t.Fatal("the second save must overwrite the first")
	}

	if _, err := s.GetNotificationPrefs(ctx, "u2"); err == nil {
		t.Fatal("expected an error for a user with no prefs")
	}
}

func TestInMemoryListNotificationPrefsIsSortedByUser(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	for _, u := range []string{"u3", "u1", "u2"} {
		if err := s.SaveNotificationPrefs(ctx, domain.NotificationPrefs{UserID: u}); err != nil {
			t.Fatalf("save %s: %v", u, err)
		}
	}

	got, err := s.ListNotificationPrefs(ctx)
	if err != nil {
		t.Fatalf("list prefs: %v", err)
	}
	// Map iteration order is random, so the sort is what keeps the notifier's
	// run deterministic.
	if len(got) != 3 || got[0].UserID != "u1" || got[1].UserID != "u2" || got[2].UserID != "u3" {
		t.Fatalf("prefs = %+v, want u1, u2, u3 in order", got)
	}
}

func TestInMemoryListNotificationPrefsIsEmptyNotNil(t *testing.T) {
	got, err := NewInMemoryStore().ListNotificationPrefs(context.Background())
	if err != nil {
		t.Fatalf("list prefs: %v", err)
	}
	if got == nil {
		t.Fatal("want an empty slice rather than nil")
	}
	if len(got) != 0 {
		t.Fatalf("got %d prefs, want none", len(got))
	}
}

func TestInMemoryNotificationLogDedupes(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	sent, err := s.NotificationSent(ctx, "u1", "2026-07-20")
	if err != nil || sent {
		t.Fatalf("sent = %v (err %v), want false before anything is recorded", sent, err)
	}

	if err := s.RecordNotificationSent(ctx, "u1", "2026-07-20", time.Now()); err != nil {
		t.Fatalf("record: %v", err)
	}
	if sent, err = s.NotificationSent(ctx, "u1", "2026-07-20"); err != nil || !sent {
		t.Fatalf("sent = %v (err %v), want true after recording", sent, err)
	}

	// The key is (user, key): neither half alone may cause a false dedupe, or
	// one user's alert would suppress another's.
	if sent, _ = s.NotificationSent(ctx, "u2", "2026-07-20"); sent {
		t.Fatal("another user must not be deduped by this user's log entry")
	}
	if sent, _ = s.NotificationSent(ctx, "u1", "2026-07-21"); sent {
		t.Fatal("another day must not be deduped")
	}
}
