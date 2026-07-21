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
	entry, err := parser.Parse(context.Background(), "/pagar 1500,50 fornecedor_medicamentos 20/07 compra mensal", time.Now())
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
	entry, err := parser.Parse(context.Background(), "/despesa 500 aluguel 10/07 Aluguel de julho", time.Now())
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
	entry, err := parser.Parse(context.Background(), "/despesa 500 aluguel", time.Now())
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
	entry, err := parser.Parse(context.Background(), "/receita 800", time.Now())
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
	if _, err := parser.Parse(context.Background(), "despesa aluguel", time.Now()); err == nil {
		t.Fatal("expected parse error for invalid command")
	}
}

func TestGeminiResponseToParsedValidatesAmountAndDueDate(t *testing.T) {
	t.Parallel()

	ref := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	entry, err := geminiResponseToParsed(geminiResponse{
		Type:        "income",
		AmountCents: 9999,
		Category:    "convenio",
		Description: "repasse",
		DueDate:     "2026-07-15",
		IsPending:   true,
	}, ref)
	if err != nil {
		t.Fatalf("geminiResponseToParsed returned error: %v", err)
	}
	if entry.Type != domain.EntryTypeIncome || !entry.IsPending {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.DueDate == nil || !entry.DueDate.Equal(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected due date: %+v", entry.DueDate)
	}

	if _, err := geminiResponseToParsed(geminiResponse{AmountCents: 0}, ref); err == nil {
		t.Fatal("expected invalid amount error")
	}
}

func TestGeminiResponseToParsedRoutesDateToTransactionDateWhenNotPending(t *testing.T) {
	t.Parallel()

	ref := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	entry, err := geminiResponseToParsed(geminiResponse{
		Type:        "expense",
		AmountCents: 50000,
		Category:    "aluguel",
		Description: "Aluguel de julho",
		DueDate:     "2026-07-10",
		IsPending:   false,
	}, ref)
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

func TestGeminiResponseToParsedRejectsDateOutOfRange(t *testing.T) {
	t.Parallel()

	ref := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		dueDate string
		wantErr bool
	}{
		{"one year in the past is accepted", "2025-07-21", false},
		{"two years in the future is accepted", "2028-07-21", false},
		{"more than one year in the past is rejected", "2024-12-31", true},
		{"more than two years in the future is rejected", "2029-01-01", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := geminiResponseToParsed(geminiResponse{
				Type:        "expense",
				AmountCents: 50000,
				Category:    "aluguel",
				Description: "aluguel",
				DueDate:     tc.dueDate,
				IsPending:   true,
			}, ref)
			if tc.wantErr && err == nil {
				t.Fatalf("expected date-out-of-range error for %s", tc.dueDate)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.dueDate, err)
			}
		})
	}
}
