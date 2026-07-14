package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func TestRegexParserParsesPendingExpenseWithDueDate(t *testing.T) {
	t.Parallel()

	parser := NewRegexParser()
	entry, err := parser.Parse(context.Background(), "/pagar 1500,50 fornecedor_medicamentos 20/07 compra mensal")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.Type != domain.EntryTypeExpense || !entry.IsPending {
		t.Fatalf("unexpected entry flags: %+v", entry)
	}
	if entry.Amount != 150050 || entry.Category != "fornecedor_medicamentos" {
		t.Fatalf("unexpected parsed values: %+v", entry)
	}
	if entry.DueDate == nil || entry.DueDate.Day() != 20 || entry.DueDate.Month() != 7 {
		t.Fatalf("expected due date in July 20, got %+v", entry.DueDate)
	}
	if entry.Description != "compra mensal" {
		t.Fatalf("expected remaining text as description, got %q", entry.Description)
	}
}

func TestRegexParserParsesExpenseWithTransactionDate(t *testing.T) {
	t.Parallel()

	parser := NewRegexParser()
	entry, err := parser.Parse(context.Background(), "/despesa 500 aluguel 10/07 Aluguel de julho")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.IsPending {
		t.Fatalf("expected non-pending entry, got %+v", entry)
	}
	if entry.Date == nil || entry.Date.Day() != 10 || entry.Date.Month() != 7 {
		t.Fatalf("expected transaction date July 10, got %+v", entry.Date)
	}
	if entry.DueDate != nil {
		t.Fatalf("expected no due date for /despesa, got %+v", entry.DueDate)
	}
	if entry.Description != "Aluguel de julho" {
		t.Fatalf("expected remaining text as description, got %q", entry.Description)
	}
}

func TestRegexParserExpenseWithoutDateLeavesDateNil(t *testing.T) {
	t.Parallel()

	parser := NewRegexParser()
	entry, err := parser.Parse(context.Background(), "/despesa 500 aluguel")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.Date != nil {
		t.Fatalf("expected nil date when none specified, got %+v", entry.Date)
	}
}

func TestRegexParserUsesDefaultCategoryAndHumanDescription(t *testing.T) {
	t.Parallel()

	parser := NewRegexParser()
	entry, err := parser.Parse(context.Background(), "/receita 800")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if entry.Type != domain.EntryTypeIncome || entry.Category != "outros_receitas" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.Description != "Outros" {
		t.Fatalf("expected human default description, got %q", entry.Description)
	}
}

func TestRegexParserRejectsInvalidCommand(t *testing.T) {
	t.Parallel()

	parser := NewRegexParser()
	if _, err := parser.Parse(context.Background(), "despesa aluguel"); err == nil {
		t.Fatal("expected parse error for invalid command")
	}
}

func TestGeminiResponseToParsedValidatesAmountAndDueDate(t *testing.T) {
	t.Parallel()

	entry, err := geminiResponseToParsed(geminiResponse{
		Type:        "income",
		AmountCents: 9999,
		Category:    "convenio",
		Description: "repasse",
		DueDate:     "2026-07-15",
		IsPending:   true,
	})
	if err != nil {
		t.Fatalf("geminiResponseToParsed returned error: %v", err)
	}
	if entry.Type != domain.EntryTypeIncome || !entry.IsPending {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.DueDate == nil || !entry.DueDate.Equal(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected due date: %+v", entry.DueDate)
	}

	if _, err := geminiResponseToParsed(geminiResponse{AmountCents: 0}); err == nil {
		t.Fatal("expected invalid amount error")
	}
}

func TestGeminiResponseToParsedRoutesDateToTransactionDateWhenNotPending(t *testing.T) {
	t.Parallel()

	entry, err := geminiResponseToParsed(geminiResponse{
		Type:        "expense",
		AmountCents: 50000,
		Category:    "aluguel",
		Description: "Aluguel de julho",
		DueDate:     "2026-07-10",
		IsPending:   false,
	})
	if err != nil {
		t.Fatalf("geminiResponseToParsed returned error: %v", err)
	}
	if entry.DueDate != nil {
		t.Fatalf("expected no due date for non-pending entry, got %+v", entry.DueDate)
	}
	if entry.Date == nil || !entry.Date.Equal(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected transaction date: %+v", entry.Date)
	}
}
