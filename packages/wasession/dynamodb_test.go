package wasession

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/dynamostore/dynamotest"
)

// The DynamoDB store's whole job is two conditional writes: one that only ever
// extends the session window, and one that succeeds exactly once per message
// id. Both are invisible to a call recorder, so these tests run them against an
// in-memory table that actually evaluates the conditions.

const testTable = "emerbot-wasession-test"

func newStore(t *testing.T) (*DynamoDBStore, *dynamotest.Table) {
	t.Helper()
	tbl := dynamotest.New(dynamotest.Config{
		Name: testTable,
		Key:  dynamotest.Key{Hash: "Phone"}, // hash-only table, TTL on ExpiresAt
	})
	return NewDynamoDBStoreWithClient(tbl, testTable), tbl
}

func TestRecordInboundOpensTheWindow(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	at := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	if err := s.RecordInbound(ctx, "+5511999", at); err != nil {
		t.Fatalf("record inbound: %v", err)
	}

	until, err := s.ActiveUntil(ctx, "+5511999")
	if err != nil {
		t.Fatalf("active until: %v", err)
	}
	if want := at.Add(Window); !until.Equal(want) {
		t.Fatalf("window closes at %v, want %v", until, want)
	}

	active, err := s.Active(ctx, "+5511999", at.Add(time.Hour))
	if err != nil || !active {
		t.Fatalf("active = %v (err %v), want true an hour in", active, err)
	}
	if active, _ = s.Active(ctx, "+5511999", at.Add(Window+time.Minute)); active {
		t.Fatal("the session must be closed once the window has passed")
	}
}

func TestRecordInboundNeverMovesExpiryBackwards(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	later := time.Date(2026, 7, 20, 15, 0, 0, 0, time.UTC)
	earlier := later.Add(-3 * time.Hour)

	if err := s.RecordInbound(ctx, "+5511999", later); err != nil {
		t.Fatalf("record later: %v", err)
	}
	// A delayed retry of an older message must be accepted (no error) but must
	// not shorten the window it would otherwise shrink.
	if err := s.RecordInbound(ctx, "+5511999", earlier); err != nil {
		t.Fatalf("record earlier must be a no-op, not an error: %v", err)
	}

	until, _ := s.ActiveUntil(ctx, "+5511999")
	if want := later.Add(Window); !until.Equal(want) {
		t.Fatalf("window closes at %v, want it kept at %v", until, want)
	}
}

func TestRecordInboundExtendsTheWindow(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	first := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	second := first.Add(2 * time.Hour)

	if err := s.RecordInbound(ctx, "+5511999", first); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := s.RecordInbound(ctx, "+5511999", second); err != nil {
		t.Fatalf("second: %v", err)
	}

	until, _ := s.ActiveUntil(ctx, "+5511999")
	if want := second.Add(Window); !until.Equal(want) {
		t.Fatalf("window closes at %v, want it extended to %v", until, want)
	}
}

func TestRecordInboundPropagatesRealErrors(t *testing.T) {
	s, tbl := newStore(t)
	boom := errors.New("dynamo is down")
	tbl.FailNext("PutItem", boom)

	// A conditional-check failure is swallowed as "already later"; anything
	// else must surface.
	if err := s.RecordInbound(context.Background(), "+55", time.Now()); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestActiveUntilIsZeroForAnUnknownPhone(t *testing.T) {
	s, _ := newStore(t)
	until, err := s.ActiveUntil(context.Background(), "+5511000")
	if err != nil {
		t.Fatalf("active until: %v", err)
	}
	if !until.IsZero() {
		t.Fatalf("until = %v, want the zero time for a phone that never messaged us", until)
	}

	active, err := s.Active(context.Background(), "+5511000", time.Now())
	if err != nil || active {
		t.Fatalf("active = %v (err %v), want false", active, err)
	}
}

func TestActiveUntilPropagatesError(t *testing.T) {
	s, tbl := newStore(t)
	boom := errors.New("read failed")
	tbl.FailNext("GetItem", boom)

	if _, err := s.ActiveUntil(context.Background(), "+55"); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}

	tbl.FailNext("GetItem", boom)
	if _, err := s.Active(context.Background(), "+55", time.Now()); !errors.Is(err, boom) {
		t.Fatalf("Active error = %v, want it to wrap %v", err, boom)
	}
}

func TestMarkProcessedDedupesRetries(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	first, err := s.MarkProcessed(ctx, "wamid.ABC", now)
	if err != nil || !first {
		t.Fatalf("first = %v (err %v), want true", first, err)
	}
	// WhatsApp re-delivers the same message id on retry; the second write must
	// lose the condition and report a duplicate.
	again, err := s.MarkProcessed(ctx, "wamid.ABC", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if again {
		t.Fatal("a repeated message id must report as already processed")
	}

	// A different id is unaffected.
	other, err := s.MarkProcessed(ctx, "wamid.XYZ", now)
	if err != nil || !other {
		t.Fatalf("other id = %v (err %v), want true", other, err)
	}
}

func TestMarkProcessedWithEmptyIDIsAlwaysFirst(t *testing.T) {
	s, tbl := newStore(t)
	ok, err := s.MarkProcessed(context.Background(), "", time.Now())
	if err != nil || !ok {
		t.Fatalf("empty id = %v (err %v), want true", ok, err)
	}
	if tbl.Calls("PutItem") != 0 {
		t.Fatal("an empty message id has nothing to dedup on and must not write")
	}
}

func TestMarkProcessedPropagatesRealErrors(t *testing.T) {
	s, tbl := newStore(t)
	boom := errors.New("dynamo is down")
	tbl.FailNext("PutItem", boom)

	if _, err := s.MarkProcessed(context.Background(), "wamid.ABC", time.Now()); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestUnmarkLetsARetryBeReprocessed(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Now()

	if _, err := s.MarkProcessed(ctx, "wamid.ABC", now); err != nil {
		t.Fatalf("mark: %v", err)
	}
	// The turn failed without a 2xx, so the marker is compensated and
	// WhatsApp's retry must be treated as new work rather than a duplicate.
	if err := s.Unmark(ctx, "wamid.ABC"); err != nil {
		t.Fatalf("unmark: %v", err)
	}

	again, err := s.MarkProcessed(ctx, "wamid.ABC", now)
	if err != nil || !again {
		t.Fatalf("after unmark = %v (err %v), want the retry to be reprocessed", again, err)
	}
}

func TestUnmarkWithEmptyIDIsANoOp(t *testing.T) {
	s, tbl := newStore(t)
	if err := s.Unmark(context.Background(), ""); err != nil {
		t.Fatalf("unmark empty: %v", err)
	}
	if tbl.Calls("DeleteItem") != 0 {
		t.Fatal("an empty message id must not issue a delete")
	}
}

func TestUnmarkPropagatesError(t *testing.T) {
	s, tbl := newStore(t)
	boom := errors.New("delete failed")
	tbl.FailNext("DeleteItem", boom)

	if err := s.Unmark(context.Background(), "wamid.ABC"); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestDedupKeysCannotCollideWithPhones(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	// A message id that looks exactly like a phone number must not close over
	// that phone's session — hence the MSGID# namespace prefix.
	if _, err := s.MarkProcessed(ctx, "5511999", now); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := s.RecordInbound(ctx, "5511999", now); err != nil {
		t.Fatalf("record inbound: %v", err)
	}

	until, _ := s.ActiveUntil(ctx, "5511999")
	if want := now.Add(Window); !until.Equal(want) {
		t.Fatalf("session window = %v, want %v — the dedup item overwrote the session", until, want)
	}
}

func TestActiveIgnoresTTLDeletionLag(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	at := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	if err := s.RecordInbound(ctx, "+5511999", at); err != nil {
		t.Fatalf("record inbound: %v", err)
	}

	// DynamoDB's TTL sweep can lag by hours, so a still-present item past its
	// ExpiresAt must read as closed rather than open.
	active, err := s.Active(ctx, "+5511999", at.Add(Window+time.Hour))
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if active {
		t.Fatal("an expired but not-yet-swept item must read as an inactive session")
	}
}
