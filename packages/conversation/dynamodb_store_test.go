package conversation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore/dynamotest"
)

const testTable = "emerbot-conversation-test"

// newStore returns a store whose clock advances one second per call, so the
// derived sort keys are chronological and deterministic.
func newStore(t *testing.T) (*DynamoDBStore, *dynamotest.Table) {
	t.Helper()
	tbl := dynamotest.New(dynamotest.Config{
		Name: testTable,
		Key:  dynamotest.Key{Hash: "PK", Range: "SK"},
	})
	clock := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	now := func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	return NewDynamoDBStoreWithClient(tbl, testTable, now), tbl
}

func msg(role domain.Role, text string, ts time.Time) domain.ConversationMessage {
	return domain.ConversationMessage{Role: role, Text: text, Timestamp: ts}
}

func appendAll(t *testing.T, s *DynamoDBStore, userID string, texts ...string) {
	t.Helper()
	ts := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	for i, text := range texts {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		if err := s.Append(context.Background(), userID, msg(role, text, ts.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatalf("append %q: %v", text, err)
		}
	}
}

func texts(messages []domain.ConversationMessage) string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.Text)
	}
	return strings.Join(out, ",")
}

func load(t *testing.T, s *DynamoDBStore, userID string, limit int) []domain.ConversationMessage {
	t.Helper()
	got, err := s.LoadRecent(context.Background(), userID, limit)
	if err != nil {
		t.Fatalf("load recent: %v", err)
	}
	return got
}

func TestAppendAndLoadRecentIsChronological(t *testing.T) {
	s, _ := newStore(t)
	appendAll(t, s, "+5511999", "oi", "olá!", "quanto gastei?", "R$ 100")

	// LoadRecent returns oldest-first, matching the in-memory store's contract.
	if got := texts(load(t, s, "+5511999", 0)); got != "oi,olá!,quanto gastei?,R$ 100" {
		t.Fatalf("turns = %q, want them oldest-first in append order", got)
	}
}

func TestLoadRecentKeepsTheNewestTurns(t *testing.T) {
	s, _ := newStore(t)
	appendAll(t, s, "+5511999", "t1", "t2", "t3", "t4", "t5")

	// The limit must drop the *oldest* turns, then hand back what remains in
	// chronological order.
	if got := texts(load(t, s, "+5511999", 2)); got != "t4,t5" {
		t.Fatalf("turns = %q, want the two most recent in order", got)
	}
}

func TestLoadRecentRoundTripsRoleAndTimestamp(t *testing.T) {
	s, _ := newStore(t)
	ts := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	if err := s.Append(context.Background(), "+5511999", msg(domain.RoleAssistant, "pronto", ts)); err != nil {
		t.Fatalf("append: %v", err)
	}

	got := load(t, s, "+5511999", 0)
	if len(got) != 1 {
		t.Fatalf("got %d turns, want 1", len(got))
	}
	if got[0].Role != domain.RoleAssistant {
		t.Fatalf("role = %q, want assistant", got[0].Role)
	}
	if !got[0].Timestamp.Equal(ts) {
		t.Fatalf("timestamp = %v, want %v", got[0].Timestamp, ts)
	}
}

func TestConversationsAreScopedPerUser(t *testing.T) {
	s, _ := newStore(t)
	appendAll(t, s, "+5511111", "meu")
	appendAll(t, s, "+5522222", "seu")

	if got := texts(load(t, s, "+5511111", 0)); got != "meu" {
		t.Fatalf("first user's turns = %q, want only their own", got)
	}
	if got := texts(load(t, s, "+5522222", 0)); got != "seu" {
		t.Fatalf("second user's turns = %q, want only their own", got)
	}
}

func TestLoadRecentIsEmptyForAnUnknownUser(t *testing.T) {
	s, _ := newStore(t)
	if got := load(t, s, "+5599999", 10); len(got) != 0 {
		t.Fatalf("got %d turns for a user with no history, want 0", len(got))
	}
}

func TestAppendSetsATTL(t *testing.T) {
	s, tbl := newStore(t)
	appendAll(t, s, "+5511999", "oi")

	items := tbl.Items()
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	expires, ok := items[0]["ExpiresAt"].(*types.AttributeValueMemberN)
	if !ok || expires.Value == "" {
		t.Fatal("every turn must carry an ExpiresAt for DynamoDB's TTL to sweep it")
	}
}

func TestAppendsInTheSameSecondDoNotOverwriteEachOther(t *testing.T) {
	tbl := dynamotest.New(dynamotest.Config{
		Name: testTable,
		Key:  dynamotest.Key{Hash: "PK", Range: "SK"},
	})
	// A frozen clock is the worst case: WhatsApp timestamps have only second
	// granularity, so the random suffix in the sort key is the only thing
	// keeping two turns from collapsing onto one item.
	frozen := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	s := NewDynamoDBStoreWithClient(tbl, testTable, func() time.Time { return frozen })

	appendAll(t, s, "+5511999", "primeira", "segunda", "terceira")

	if got := len(load(t, s, "+5511999", 0)); got != 3 {
		t.Fatalf("got %d turns appended within one instant, want all 3", got)
	}
}

func TestAppendPropagatesError(t *testing.T) {
	s, tbl := newStore(t)
	boom := errors.New("write failed")
	tbl.FailNext("PutItem", boom)

	err := s.Append(context.Background(), "+55", msg(domain.RoleUser, "oi", time.Now()))
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestLoadRecentPropagatesError(t *testing.T) {
	s, tbl := newStore(t)
	boom := errors.New("query failed")
	tbl.FailNext("Query", boom)

	if _, err := s.LoadRecent(context.Background(), "+55", 10); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestLoadRecentSkipsUnreadableTurns(t *testing.T) {
	s, tbl := newStore(t)
	appendAll(t, s, "+5511999", "boa")

	// A turn whose Text is a map rather than a string must drop out of context
	// instead of failing the whole load — history is best-effort.
	err := tbl.Seed(map[string]types.AttributeValue{
		"PK":   &types.AttributeValueMemberS{Value: "+5511999"},
		"SK":   &types.AttributeValueMemberS{Value: "9999999999999999999#ffff"},
		"Role": &types.AttributeValueMemberS{Value: "user"},
		"Text": &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
			"unexpected": &types.AttributeValueMemberS{Value: "shape"},
		}},
		"Timestamp": &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got := texts(load(t, s, "+5511999", 0)); got != "boa" {
		t.Fatalf("turns = %q, want the readable turn to survive alone", got)
	}
}

func TestSortKeyIsLexicographicallyChronological(t *testing.T) {
	instant := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	early := sortKey(instant)
	late := sortKey(instant.Add(time.Second))

	// Zero padding to a fixed width is what makes string order match time
	// order; without it "9..." would sort above "10...".
	if early >= late {
		t.Fatalf("sort keys are not chronological: %q >= %q", early, late)
	}
	if len(early) != len(late) {
		t.Fatalf("sort keys have differing widths (%d vs %d), so string order can diverge from time order", len(early), len(late))
	}
	// Two turns arriving in the same nanosecond must still get distinct keys,
	// which is what the random suffix is there for.
	first, second := sortKey(instant), sortKey(instant)
	if first == second {
		t.Fatalf("two keys for the same instant collided (%q); the random suffix is not doing its job", first)
	}
}
