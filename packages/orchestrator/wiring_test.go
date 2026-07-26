package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/finance"
)

// NewTextGenerator decides which LLM (if any) the bot talks to, and every
// wrong branch degrades silently to a canned reply — so the selection itself
// is worth pinning down.

func TestNewTextGeneratorFallsBackToStaticWithoutAFinanceStore(t *testing.T) {
	// Every real agent needs the finance store for its tools; without one
	// there is nothing to generate against.
	cases := map[string]Config{
		"no store, no provider": {},
		"no store, but a key":   {GeminiAPIKey: "key"},
		"no store, but ollama":  {LLMProvider: "ollama", OllamaHost: "http://localhost:11434"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := NewTextGenerator(cfg).(StaticClient); !ok {
				t.Fatalf("generator = %T, want StaticClient", NewTextGenerator(cfg))
			}
		})
	}
}

func TestNewTextGeneratorSelectsOllama(t *testing.T) {
	gen := NewTextGenerator(Config{
		FinanceStore: finance.NewInMemoryStore(),
		LLMProvider:  "ollama",
		OllamaHost:   "http://localhost:11434",
		OllamaModel:  "llama3",
		// A Gemini key must not win over an explicit provider choice.
		GeminiAPIKey: "should-be-ignored",
	})
	if _, ok := gen.(*agentGenerator); !ok {
		t.Fatalf("generator = %T, want an agent generator for the ollama provider", gen)
	}
}

func TestNewTextGeneratorFallsBackToStaticWithoutAGeminiKey(t *testing.T) {
	gen := NewTextGenerator(Config{FinanceStore: finance.NewInMemoryStore()})
	if _, ok := gen.(StaticClient); !ok {
		t.Fatalf("generator = %T, want StaticClient with no provider and no key", gen)
	}
}

func TestNewServiceUsesAnInjectedShortTermStore(t *testing.T) {
	injected := NewInMemoryStores()
	svc := NewService(Config{ShortTerm: injected})

	if svc.shortTerm != ShortTermStore(injected) {
		t.Fatal("NewService must use the injected short-term store")
	}
	// The long-term store is always the built-in one, and the defaults must be
	// set or HandleMessage would load zero history and answer with "".
	if svc.longTerm == nil || svc.shortTermLimit == 0 || svc.defaultResponder == "" {
		t.Fatalf("service = %+v, want the long-term store, limit and fallback populated", svc)
	}
}

func TestNewServiceDefaultsToItsOwnShortTermStore(t *testing.T) {
	svc := NewService(Config{})
	if svc.shortTerm == nil {
		t.Fatal("NewService must fall back to its in-memory short-term store")
	}
	// With no finance store the generator degrades to the static responder.
	if _, ok := svc.generator.(StaticClient); !ok {
		t.Fatalf("generator = %T, want StaticClient", svc.generator)
	}
}

// --- validateMessage ---

func TestHandleMessageValidation(t *testing.T) {
	valid := domain.Message{UserID: "u1", MessageID: "m1", Text: "oi", Timestamp: time.Now()}

	cases := map[string]struct {
		mutate  func(*domain.Message)
		wantErr string
	}{
		"missing user id":    {func(m *domain.Message) { m.UserID = "" }, "user id is required"},
		"blank user id":      {func(m *domain.Message) { m.UserID = "   " }, "user id is required"},
		"missing message id": {func(m *domain.Message) { m.MessageID = "" }, "message id is required"},
		"blank message id":   {func(m *domain.Message) { m.MessageID = "\t" }, "message id is required"},
		"missing timestamp":  {func(m *domain.Message) { m.Timestamp = time.Time{} }, "timestamp is required"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc := newTestService(stubLLM{output: Output{Text: "ok"}})
			msg := valid
			tc.mutate(&msg)

			_, err := svc.HandleMessage(context.Background(), msg)
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}

	t.Run("a valid message is accepted", func(t *testing.T) {
		svc := newTestService(stubLLM{output: Output{Text: "ok"}})
		if _, err := svc.HandleMessage(context.Background(), valid); err != nil {
			t.Fatalf("a valid message was rejected: %v", err)
		}
	})
}

// --- in-memory stores ---

func TestInMemoryStoresSaveIsAnUpsertKeyedByMemoryKey(t *testing.T) {
	stores := NewInMemoryStores()
	ctx := context.Background()

	first := domain.Memory{UserID: "u1", Type: "pref", ID: "bebida", Value: "gosta de café"}
	if err := stores.Save(ctx, first); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Re-saving the same key replaces rather than appending, or a user's
	// long-term memory would grow a duplicate on every update.
	updated := first
	updated.Value = "gosta de chá"
	if err := stores.Save(ctx, updated); err != nil {
		t.Fatalf("re-save: %v", err)
	}

	got, err := stores.LoadByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d memories, want 1 after an update: %+v", len(got), got)
	}
	if got[0].Value != "gosta de chá" {
		t.Fatalf("value = %q, want the updated value", got[0].Value)
	}
}

func TestInMemoryStoresSaveKeepsMemoriesSortedAndPerUser(t *testing.T) {
	stores := NewInMemoryStores()
	ctx := context.Background()

	for _, m := range []domain.Memory{
		{UserID: "u1", Type: "pref", ID: "zeta", Value: "z"},
		{UserID: "u1", Type: "pref", ID: "alfa", Value: "a"},
		{UserID: "u2", Type: "pref", ID: "outro", Value: "o"},
	} {
		if err := stores.Save(ctx, m); err != nil {
			t.Fatalf("save %s: %v", m.Key(), err)
		}
	}

	got, err := stores.LoadByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d memories for u1, want 2 — another user's memory leaked in", len(got))
	}
	// Sorted by key so the prompt the model sees is stable across runs.
	if got[0].Key() > got[1].Key() {
		t.Fatalf("memories are unsorted: %q before %q", got[0].Key(), got[1].Key())
	}

	other, _ := stores.LoadByUser(ctx, "u2")
	if len(other) != 1 {
		t.Fatalf("got %d memories for u2, want 1", len(other))
	}
	if empty, _ := stores.LoadByUser(ctx, "u3"); len(empty) != 0 {
		t.Fatalf("got %d memories for an unknown user, want none", len(empty))
	}
}

func TestInMemoryStoresLoadRecentAppliesTheLimit(t *testing.T) {
	stores := NewInMemoryStores()
	ctx := context.Background()
	for _, text := range []string{"t1", "t2", "t3"} {
		if err := stores.Append(ctx, "u1", domain.ConversationMessage{Role: domain.RoleUser, Text: text}); err != nil {
			t.Fatalf("append %s: %v", text, err)
		}
	}

	got, err := stores.LoadRecent(ctx, "u1", 2)
	if err != nil {
		t.Fatalf("load recent: %v", err)
	}
	// The limit keeps the newest turns, in chronological order.
	if len(got) != 2 || got[0].Text != "t2" || got[1].Text != "t3" {
		t.Fatalf("turns = %+v, want the last two in order", got)
	}

	all, _ := stores.LoadRecent(ctx, "u1", 0)
	if len(all) != 3 {
		t.Fatalf("got %d turns with no limit, want 3", len(all))
	}
}

// --- generator error propagation ---

func TestHandleMessageSurfacesAGeneratorError(t *testing.T) {
	svc := newTestService(stubLLM{err: errors.New("llm exploded")})

	_, err := svc.HandleMessage(context.Background(), domain.Message{
		UserID: "u1", MessageID: "m1", Text: "oi", Timestamp: time.Now(),
	})
	if err == nil {
		t.Fatal("expected the generator's failure to reach the caller")
	}
}
