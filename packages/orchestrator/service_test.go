package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
)

func TestHandleMessageRejectsInvalidMessage(t *testing.T) {
	t.Parallel()

	service := newTestService(stubLLM{output: Output{Text: "ok"}})
	_, err := service.HandleMessage(context.Background(), domain.Message{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestHandleMessageUsesDefaultResponseWhenLLMReturnsBlank(t *testing.T) {
	t.Parallel()

	service := newTestService(stubLLM{output: Output{Text: "   "}})
	response, err := service.HandleMessage(context.Background(), validMessage("oi"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if response.Text != "Não consegui gerar uma resposta." {
		t.Fatalf("unexpected fallback response: %q", response.Text)
	}
}

// TestGeminiGeneratorUsesSharedLedgerRegardlessOfSender proves the finance
// agent always sees shared.FinanceLedgerID, not the sender's own user ID —
// otherwise each sender's natural-language entries would land in their own
// isolated ledger instead of the one slash commands (e.g. /resumo) read from.
func TestGeminiGeneratorUsesSharedLedgerRegardlessOfSender(t *testing.T) {
	t.Parallel()

	agent := &fakeFinanceAgent{reply: "ok"}
	gen := &agentGenerator{agent: agent}

	history := []domain.ConversationMessage{
		{Role: domain.RoleUser, Text: "oi", Timestamp: time.Now()},
		{Role: domain.RoleAssistant, Text: "olá", Timestamp: time.Now()},
		{Role: domain.RoleUser, Text: "paguei 50 de aluguel", Timestamp: time.Now()},
	}
	_, err := gen.Generate(context.Background(), Input{
		UserMessage: domain.Message{
			UserID:    "5511999999999",
			Text:      "paguei 50 de aluguel",
			Timestamp: time.Now(),
		},
		ShortTerm: history,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if agent.gotUserID != shared.FinanceLedgerID {
		t.Fatalf("expected agent to receive shared ledger id %q, got %q", shared.FinanceLedgerID, agent.gotUserID)
	}
	// The whole short-term history must reach the agent, not just the last turn,
	// so the model keeps context across messages.
	if len(agent.gotHistory) != len(history) {
		t.Fatalf("expected agent to receive %d history turns, got %d", len(history), len(agent.gotHistory))
	}
}

// TestNewTextGeneratorSelectsProvider proves the provider switch: LLMProvider
// "ollama" builds a real agent, and a missing finance store always degrades to
// the static responder (an agent has no tools to run without it).
func TestNewTextGeneratorSelectsProvider(t *testing.T) {
	t.Parallel()

	ollamaGen := NewTextGenerator(Config{FinanceStore: finance.NewInMemoryStore(), LLMProvider: "ollama"})
	if _, ok := ollamaGen.(*agentGenerator); !ok {
		t.Fatalf("expected *agentGenerator for ollama provider, got %T", ollamaGen)
	}

	staticGen := NewTextGenerator(Config{LLMProvider: "ollama"})
	if _, ok := staticGen.(StaticClient); !ok {
		t.Fatalf("expected StaticClient fallback without a finance store, got %T", staticGen)
	}
}

type fakeFinanceAgent struct {
	reply      string
	gotUserID  string
	gotHistory []domain.ConversationMessage
}

func (f *fakeFinanceAgent) Process(_ context.Context, userID string, history []domain.ConversationMessage, _ time.Time) (string, error) {
	f.gotUserID = userID
	f.gotHistory = history
	return f.reply, nil
}

type stubLLM struct {
	output Output
	err    error
}

func (s stubLLM) Generate(_ context.Context, _ Input) (Output, error) {
	if s.err != nil {
		return Output{}, s.err
	}
	return s.output, nil
}

func newTestService(gen TextGenerator) *Service {
	return NewServiceWithGenerator(gen)
}

func validMessage(text string) domain.Message {
	return domain.Message{
		UserID:    "u1",
		Text:      text,
		Timestamp: time.Now().UTC(),
		MessageID: "m1",
	}
}
