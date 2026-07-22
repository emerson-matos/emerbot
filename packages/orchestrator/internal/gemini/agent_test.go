package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/finance"
)

type scriptedGenerator struct {
	responses []*genai.GenerateContentResponse
	err       error
	calls     int
	lastTools []*genai.Tool
}

func (g *scriptedGenerator) GenerateContent(_ context.Context, _ string, _ []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	if config != nil {
		g.lastTools = config.Tools
	}
	if g.err != nil {
		g.calls++
		return nil, g.err
	}
	if g.calls >= len(g.responses) {
		g.calls++
		return g.responses[len(g.responses)-1], nil
	}
	r := g.responses[g.calls]
	g.calls++
	return r, nil
}

func textResponse(text string) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: text}}}},
		},
	}
}

func functionCallResponse(name string, args map[string]any) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Role: "model", Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: name, Args: args}},
			}}},
		},
	}
}

func newTestAgent(gen contentGenerator, store finance.Store) *Agent {
	financeTools := finance.FinanceTools(store)
	genaiTools := make([]*genai.Tool, len(financeTools))
	handlers := make(map[string]func(context.Context, string, json.RawMessage) (any, error), len(financeTools))
	for i, t := range financeTools {
		genaiTools[i] = &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: t.Name, Description: t.Description, Parameters: t.Parameters,
			}},
		}
		handlers[t.Name] = t.Handler
	}
	return &Agent{gen: gen, model: model, tools: genaiTools, toolHandlers: handlers}
}

func TestAgentReturnsTextForChitChat(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		textResponse("Sou um assistente financeiro e posso ajudar com o fluxo de caixa."),
	}}
	agent := newTestAgent(gen, finance.NewInMemoryStore())

	reply, err := agent.Process(context.Background(), "u1", "oi, tudo bem?", time.Now())
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if !strings.Contains(reply, "assistente financeiro") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if gen.calls != 1 {
		t.Fatalf("expected a single Gemini call for chit-chat, got %d", gen.calls)
	}
}

func TestAgentCreatesEntryViaTool(t *testing.T) {
	t.Parallel()

	store := finance.NewInMemoryStore()
	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		functionCallResponse("create_financial_entry", map[string]any{
			"type":       "expense",
			"amount":     500.0,
			"category":   "aluguel",
			"is_pending": false,
		}),
		textResponse("✅ Despesa de R$500,00 em aluguel registrada."),
	}}
	agent := newTestAgent(gen, store)

	reply, err := agent.Process(context.Background(), "ledger", "paguei 500 de aluguel", time.Now())
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if !strings.Contains(reply, "registrada") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 Gemini calls (tool + final), got %d", gen.calls)
	}

	entries, err := store.ListEntries(context.Background(), "ledger", finance.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected the tool to persist 1 entry, got %d", len(entries))
	}
	if entries[0].Amount != 50000 || entries[0].Category != "aluguel" {
		t.Fatalf("unexpected saved entry: %+v", entries[0])
	}
	if entries[0].Type != domain.EntryTypeExpense || entries[0].PaymentStatus != domain.PaymentStatusPaid {
		t.Fatalf("unexpected type/status: %+v", entries[0])
	}
}

func TestAgentAnswersSummaryQuery(t *testing.T) {
	t.Parallel()

	store := finance.NewInMemoryStore()
	month := time.Now().UTC().Format("2006-01")
	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		functionCallResponse("get_month_summary", map[string]any{"month": month}),
		textResponse("Este mês: R$0,00 de saldo."),
	}}
	agent := newTestAgent(gen, store)

	reply, err := agent.Process(context.Background(), "u1", "como estamos este mês?", time.Now())
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if reply == "" {
		t.Fatal("expected a non-empty reply")
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 Gemini calls, got %d", gen.calls)
	}
}

func TestAgentChainsMultipleToolRounds(t *testing.T) {
	t.Parallel()

	store := finance.NewInMemoryStore()
	month := time.Now().UTC().Format("2006-01")
	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		functionCallResponse("create_financial_entry", map[string]any{
			"type": "expense", "amount": 500.0, "category": "aluguel", "is_pending": false,
		}),
		functionCallResponse("get_month_summary", map[string]any{"month": month}),
		textResponse("Registrei e o saldo do mês está atualizado."),
	}}
	agent := newTestAgent(gen, store)

	reply, err := agent.Process(context.Background(), "ledger", "paguei 500 de aluguel, como ficou o mês?", time.Now())
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if reply == "" {
		t.Fatal("expected a non-empty reply")
	}
	if gen.calls != 3 {
		t.Fatalf("expected 3 Gemini calls across two tool rounds + final, got %d", gen.calls)
	}
	entries, _ := store.ListEntries(context.Background(), "ledger", finance.EntryFilter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 persisted entry, got %d", len(entries))
	}
}

func TestAgentRecoversFromToolError(t *testing.T) {
	t.Parallel()

	store := finance.NewInMemoryStore()
	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		functionCallResponse("create_financial_entry", map[string]any{
			"type": "expense", "amount": 0.0, "category": "aluguel", "is_pending": false,
		}),
		textResponse("Desculpe, não consegui registrar: valor inválido."),
	}}
	agent := newTestAgent(gen, store)

	reply, err := agent.Process(context.Background(), "ledger", "gastei nada em aluguel", time.Now())
	if err != nil {
		t.Fatalf("expected recovery, got error: %v", err)
	}
	if !strings.Contains(reply, "Desculpe") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 Gemini calls (failed tool + recovery), got %d", gen.calls)
	}
	entries, _ := store.ListEntries(context.Background(), "ledger", finance.EntryFilter{})
	if len(entries) != 0 {
		t.Fatalf("expected no entry persisted after a rejected amount, got %d", len(entries))
	}
}

func TestAgentExposesAllFinanceTools(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{textResponse("ok")}}
	agent := newTestAgent(gen, finance.NewInMemoryStore())

	if _, err := agent.Process(context.Background(), "u1", "oi", time.Now()); err != nil {
		t.Fatalf("Process: %v", err)
	}

	var names []string
	for _, tool := range gen.lastTools {
		for _, decl := range tool.FunctionDeclarations {
			names = append(names, decl.Name)
		}
	}
	for _, want := range []string{"create_financial_entry", "get_month_summary", "list_due_entries", "search_entries"} {
		if !contains(names, want) {
			t.Fatalf("expected tool %q to be exposed, got %v", want, names)
		}
	}
}

func TestAgentErrorsOnUnknownTool(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		functionCallResponse("delete_everything", map[string]any{}),
	}}
	agent := newTestAgent(gen, finance.NewInMemoryStore())

	if _, err := agent.Process(context.Background(), "u1", "apague tudo", time.Now()); err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
}

func TestAgentStopsAfterMaxToolRounds(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		functionCallResponse("get_month_summary", map[string]any{}),
	}}
	agent := newTestAgent(gen, finance.NewInMemoryStore())

	_, err := agent.Process(context.Background(), "u1", "loop", time.Now())
	if err == nil {
		t.Fatal("expected an error when the tool-calling loop never terminates")
	}
	if gen.calls != maxToolRounds {
		t.Fatalf("expected exactly %d rounds, got %d", maxToolRounds, gen.calls)
	}
}

func TestAgentPropagatesGeneratorError(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{err: errors.New("network down")}
	agent := newTestAgent(gen, finance.NewInMemoryStore())

	if _, err := agent.Process(context.Background(), "u1", "gastei 50 no mercado", time.Now()); err == nil {
		t.Fatal("expected the generator error to propagate")
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
