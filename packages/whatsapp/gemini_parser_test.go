package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
)

// fakeContentGenerator is a test double for contentGenerator: it returns a
// canned response/error and counts calls, so tests can assert the regex
// fast-path skips Gemini entirely for well-structured commands.
type fakeContentGenerator struct {
	resp  *genai.GenerateContentResponse
	err   error
	calls int
}

func (f *fakeContentGenerator) GenerateContent(_ context.Context, _ string, _ []*genai.Content, _ *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

// geminiResponseText builds a *genai.GenerateContentResponse whose single
// candidate/part carries the given raw text, mimicking what the SDK returns.
func geminiResponseText(text string) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{Content: &genai.Content{Parts: []*genai.Part{{Text: text}}}},
		},
	}
}

func TestGeminiParserParsesCleanJSONPendingEntry(t *testing.T) {
	t.Parallel()

	gen := &fakeContentGenerator{resp: geminiResponseText(
		`{"type":"expense","amount_cents":30000,"category":"energia_agua","description":"Conta de luz","due_date":"2026-07-20","is_pending":true,"is_financial":true}`,
	)}
	parser := &GeminiParser{gen: gen, model: geminiModel}

	entry, err := parser.Parse(context.Background(), "vou pagar 300 de luz dia 20/07", time.Now())
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.Type != domain.EntryTypeExpense || !entry.IsPending {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.DueDate == nil || entry.DueDate.Day() != 20 || entry.DueDate.Month() != 7 {
		t.Fatalf("expected due date July 20, got %+v", entry.DueDate)
	}
	if entry.Date != nil {
		t.Fatalf("expected no transaction date for pending entry, got %+v", entry.Date)
	}
	if gen.calls != 1 {
		t.Fatalf("expected exactly one Gemini call, got %d", gen.calls)
	}
}

func TestGeminiParserParsesCleanJSONNonPendingEntry(t *testing.T) {
	t.Parallel()

	gen := &fakeContentGenerator{resp: geminiResponseText(
		`{"type":"income","amount_cents":80000,"category":"venda_balcao","description":"Venda balcão","due_date":"2026-07-10","is_pending":false,"is_financial":true}`,
	)}
	parser := &GeminiParser{gen: gen, model: geminiModel}

	entry, err := parser.Parse(context.Background(), "recebi 800 de venda ontem", time.Now())
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.Type != domain.EntryTypeIncome || entry.IsPending {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.Date == nil || entry.Date.Day() != 10 || entry.Date.Month() != 7 {
		t.Fatalf("expected transaction date July 10, got %+v", entry.Date)
	}
	if entry.DueDate != nil {
		t.Fatalf("expected no due date for non-pending entry, got %+v", entry.DueDate)
	}
}

func TestGeminiParserParsesFencedJSON(t *testing.T) {
	t.Parallel()

	gen := &fakeContentGenerator{resp: geminiResponseText(
		"```json\n{\"type\":\"expense\",\"amount_cents\":5000,\"category\":\"aluguel\",\"description\":\"Aluguel\",\"due_date\":\"\",\"is_pending\":false,\"is_financial\":true}\n```",
	)}
	parser := &GeminiParser{gen: gen, model: geminiModel}

	entry, err := parser.Parse(context.Background(), "paguei 50 de aluguel", time.Now())
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.Amount != 5000 || entry.Category != "aluguel" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestGeminiParserReturnsErrNotFinancialForChitChat(t *testing.T) {
	t.Parallel()

	gen := &fakeContentGenerator{resp: geminiResponseText(`{"is_financial":false}`)}
	parser := &GeminiParser{gen: gen, model: geminiModel}

	_, err := parser.Parse(context.Background(), "oi, tudo bem?", time.Now())
	if !errors.Is(err, ErrNotFinancial) {
		t.Fatalf("expected ErrNotFinancial, got %v", err)
	}
}

func TestGeminiParserRejectsNonPositiveAmount(t *testing.T) {
	t.Parallel()

	gen := &fakeContentGenerator{resp: geminiResponseText(
		`{"type":"expense","amount_cents":0,"category":"aluguel","description":"x","is_pending":false,"is_financial":true}`,
	)}
	parser := &GeminiParser{gen: gen, model: geminiModel}

	if _, err := parser.Parse(context.Background(), "gastei nada", time.Now()); err == nil {
		t.Fatal("expected error for non-positive amount")
	}
}

func TestGeminiParserSkipsGeminiForSlashCommands(t *testing.T) {
	t.Parallel()

	gen := &fakeContentGenerator{resp: geminiResponseText(`{}`)}
	parser := &GeminiParser{gen: gen, model: geminiModel}

	entry, err := parser.Parse(context.Background(), "/despesa 500 aluguel", time.Now())
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.Amount != 50000 {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if gen.calls != 0 {
		t.Fatalf("expected regex fast-path to skip Gemini, got %d calls", gen.calls)
	}
}

func TestGeminiParserPropagatesGeneratorError(t *testing.T) {
	t.Parallel()

	gen := &fakeContentGenerator{err: errors.New("network down")}
	parser := &GeminiParser{gen: gen, model: geminiModel}

	if _, err := parser.Parse(context.Background(), "gastei 50 no mercado", time.Now()); err == nil {
		t.Fatal("expected error to propagate from generator")
	}
}
