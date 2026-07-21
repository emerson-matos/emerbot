package wasession

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryActiveWithinWindow(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	at := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	if err := s.RecordInbound(ctx, "5511999999999", at); err != nil {
		t.Fatal(err)
	}

	// Just before the window closes -> active.
	beforeClose := at.Add(Window - time.Minute)
	if ok, _ := s.Active(ctx, "5511999999999", beforeClose); !ok {
		t.Fatal("expected session active just before window close")
	}
	// At/after expiry -> inactive.
	if ok, _ := s.Active(ctx, "5511999999999", at.Add(Window)); ok {
		t.Fatal("expected session inactive at window close")
	}
}

func TestInMemoryActiveUnknownPhone(t *testing.T) {
	s := NewInMemoryStore()
	if ok, _ := s.Active(context.Background(), "000", time.Now()); ok {
		t.Fatal("unknown phone must not be active")
	}
}

func TestInMemoryMarkProcessedDedups(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)

	first, err := s.MarkProcessed(ctx, "wamid.ABC", now)
	if err != nil {
		t.Fatal(err)
	}
	if !first {
		t.Fatal("first delivery of a message ID must report first=true")
	}

	// A retry of the same ID within the dedup window is a duplicate.
	again, err := s.MarkProcessed(ctx, "wamid.ABC", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("a retry of the same message ID must report first=false")
	}

	// An empty ID has nothing to dedup on and always processes.
	if ok, _ := s.MarkProcessed(ctx, "", now); !ok {
		t.Fatal("empty message ID must report first=true")
	}
}

func TestInMemoryRecordOnlyExtends(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStore()

	newer := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	older := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	if err := s.RecordInbound(ctx, "p", newer); err != nil {
		t.Fatal(err)
	}
	// A late retry of an older message must not shorten the window.
	if err := s.RecordInbound(ctx, "p", older); err != nil {
		t.Fatal(err)
	}
	// Still active at a point only the newer message's window covers.
	at := older.Add(Window).Add(time.Minute)
	if ok, _ := s.Active(ctx, "p", at); !ok {
		t.Fatal("older retry should not have shortened the window")
	}
}
