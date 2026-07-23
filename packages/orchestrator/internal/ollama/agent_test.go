package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/finance"
)

// scriptedOllama is a fake Ollama /api/chat that returns canned responses in
// order and records the request bodies it received.
type scriptedOllama struct {
	responses []chatResponse
	calls     int
	bodies    [][]byte
}

func (s *scriptedOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.bodies = append(s.bodies, body)

	idx := s.calls
	s.calls++
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.responses[idx])
}

func textMessage(text string) chatResponse {
	return chatResponse{Done: true, Message: message{Role: "assistant", Content: text}}
}

func toolCallMessage(name string, args map[string]any) chatResponse {
	return chatResponse{Message: message{
		Role:      "assistant",
		ToolCalls: []toolCall{{Function: toolCallFunction{Name: name, Arguments: args}}},
	}}
}

func userTurn(text string) []domain.ConversationMessage {
	return []domain.ConversationMessage{{Role: domain.RoleUser, Text: text, Timestamp: time.Now()}}
}

func TestAgentReturnsTextForChitChat(t *testing.T) {
	t.Parallel()

	script := &scriptedOllama{responses: []chatResponse{
		textMessage("Sou um assistente financeiro e posso ajudar com o fluxo de caixa."),
	}}
	server := httptest.NewServer(script)
	defer server.Close()

	agent := NewAgent(server.URL, "test-model", finance.NewInMemoryStore(), "")
	reply, err := agent.Process(context.Background(), "u1", userTurn("oi, tudo bem?"), time.Now())
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(reply, "assistente financeiro") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if script.calls != 1 {
		t.Fatalf("expected a single call for chit-chat, got %d", script.calls)
	}
}

func TestAgentCreatesEntryViaTool(t *testing.T) {
	t.Parallel()

	store := finance.NewInMemoryStore()
	script := &scriptedOllama{responses: []chatResponse{
		toolCallMessage("create_financial_entry", map[string]any{
			"type":       "expense",
			"amount":     500.0,
			"category":   "aluguel",
			"is_pending": false,
		}),
		textMessage("✅ Despesa de R$500,00 em aluguel registrada."),
	}}
	server := httptest.NewServer(script)
	defer server.Close()

	agent := NewAgent(server.URL, "test-model", store, "")
	reply, err := agent.Process(context.Background(), "ledger", userTurn("paguei 500 de aluguel"), time.Now())
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.Contains(reply, "registrada") {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if script.calls != 2 {
		t.Fatalf("expected 2 calls (tool + final), got %d", script.calls)
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

func TestAgentThreadsHistoryAndSystemPrompt(t *testing.T) {
	t.Parallel()

	script := &scriptedOllama{responses: []chatResponse{textMessage("Seu nome é Emerson.")}}
	server := httptest.NewServer(script)
	defer server.Close()

	agent := NewAgent(server.URL, "test-model", finance.NewInMemoryStore(), "")
	history := []domain.ConversationMessage{
		{Role: domain.RoleUser, Text: "meu nome é Emerson", Timestamp: time.Now()},
		{Role: domain.RoleAssistant, Text: "Prazer, Emerson!", Timestamp: time.Now()},
		{Role: domain.RoleUser, Text: "qual é o meu nome?", Timestamp: time.Now()},
	}
	if _, err := agent.Process(context.Background(), "u1", history, time.Now()); err != nil {
		t.Fatalf("Process: %v", err)
	}

	var sent chatRequest
	if err := json.Unmarshal(script.bodies[0], &sent); err != nil {
		t.Fatalf("decode sent request: %v", err)
	}
	// system + 3 history turns.
	if len(sent.Messages) != 4 {
		t.Fatalf("expected 4 messages sent, got %d", len(sent.Messages))
	}
	if sent.Messages[0].Role != "system" || !strings.Contains(sent.Messages[0].Content, "assistente financeiro") {
		t.Fatalf("first message should be the finance system prompt, got %+v", sent.Messages[0])
	}
	if sent.Messages[1].Role != "user" || sent.Messages[2].Role != "assistant" || sent.Messages[3].Role != "user" {
		t.Fatalf("unexpected history roles: %q %q %q",
			sent.Messages[1].Role, sent.Messages[2].Role, sent.Messages[3].Role)
	}
	// Tools must be advertised so the model can call them.
	if len(sent.Tools) == 0 {
		t.Fatal("expected finance tools to be sent to Ollama")
	}
}

func TestSchemaToJSONConvertsFinanceTool(t *testing.T) {
	t.Parallel()

	var createTool finance.Tool
	for _, tl := range finance.FinanceTools(finance.NewInMemoryStore(), "") {
		if tl.Name == "create_financial_entry" {
			createTool = tl
		}
	}
	if createTool.Name == "" {
		t.Fatal("create_financial_entry tool not found")
	}

	got := schemaToJSON(createTool.Parameters)
	if got["type"] != "object" {
		t.Fatalf("root type should be lowercase 'object', got %v", got["type"])
	}
	props, ok := got["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %T", got["properties"])
	}
	amount, ok := props["amount"].(map[string]any)
	if !ok {
		t.Fatalf("expected amount schema, got %T", props["amount"])
	}
	if amount["type"] != "number" {
		t.Fatalf("amount type should be lowercase 'number', got %v", amount["type"])
	}
	req, ok := got["required"].([]string)
	if !ok || len(req) == 0 {
		t.Fatalf("expected non-empty required list, got %v", got["required"])
	}

	// Enums are the main constraint the model must respect — a regression that
	// dropped them (unconstraining category/type) must fail here.
	typeField, ok := props["type"].(map[string]any)
	if !ok {
		t.Fatalf("expected type schema, got %T", props["type"])
	}
	enum, ok := typeField["enum"].([]string)
	if !ok || !containsStr(enum, "expense") || !containsStr(enum, "income") {
		t.Fatalf("type enum should carry expense/income, got %v", typeField["enum"])
	}
}

// TestSchemaToJSONLowercasesIntegerType covers the one integer-typed field
// (list_due_entries.limit) so the STRING/INTEGER→lowercase mapping stays covered.
func TestSchemaToJSONLowercasesIntegerType(t *testing.T) {
	t.Parallel()

	var due finance.Tool
	for _, tl := range finance.FinanceTools(finance.NewInMemoryStore(), "") {
		if tl.Name == "list_due_entries" {
			due = tl
		}
	}
	if due.Name == "" {
		t.Fatal("list_due_entries tool not found")
	}

	props, _ := schemaToJSON(due.Parameters)["properties"].(map[string]any)
	limit, ok := props["limit"].(map[string]any)
	if !ok {
		t.Fatalf("expected limit schema, got %T", props["limit"])
	}
	if limit["type"] != "integer" {
		t.Fatalf("limit type should be lowercase 'integer', got %v", limit["type"])
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
