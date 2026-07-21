package finance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func handlerFor(t *testing.T, store Store, name string) ToolHandler {
	t.Helper()
	_, handlers := FinanceTools(store)
	h, ok := handlers[name]
	if !ok {
		t.Fatalf("tool %q not registered", name)
	}
	return h
}

func callTool(t *testing.T, h ToolHandler, userID string, args map[string]any) any {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := h(context.Background(), userID, raw)
	if err != nil {
		t.Fatalf("tool returned error: %v", err)
	}
	return out
}

func TestCreateEntryToolPersistsExpense(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	callTool(t, h, "u1", map[string]any{
		"type":        "expense",
		"amount":      19.99,
		"category":    "fornecedor_geral",
		"description": "Sacola",
		"is_pending":  false,
	})

	entries, err := store.ListEntries(context.Background(), "u1", EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	// 19.99 must round to 1999 centavos, not truncate to 1998.
	if e.Amount != 1999 {
		t.Fatalf("expected 1999 centavos, got %d", e.Amount)
	}
	if e.Type != domain.EntryTypeExpense || e.PaymentStatus != domain.PaymentStatusPaid {
		t.Fatalf("unexpected type/status: %+v", e)
	}
	if e.PaymentDate == nil {
		t.Fatal("expected PaymentDate set for a paid entry")
	}
}

func TestCreateEntryToolPendingWithDueDate(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	callTool(t, h, "u1", map[string]any{
		"type":       "expense",
		"amount":     300.0,
		"category":   "energia_agua",
		"due_date":   "2026-08-20",
		"is_pending": true,
	})

	entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.PaymentStatus != domain.PaymentStatusPending {
		t.Fatalf("expected pending, got %s", e.PaymentStatus)
	}
	if e.PaymentDate != nil {
		t.Fatal("expected no PaymentDate for a pending entry")
	}
	if e.DueDate == nil || !e.DueDate.Equal(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected due date: %+v", e.DueDate)
	}
}

func TestCreateEntryToolRejectsNonPositiveAmount(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	raw, _ := json.Marshal(map[string]any{
		"type": "expense", "amount": 0.0, "category": "aluguel", "is_pending": false,
	})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected an error for a non-positive amount")
	}
}

func TestCreateEntryToolRejectsAmountOverCap(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	raw, _ := json.Marshal(map[string]any{
		"type": "expense", "amount": maxEntryAmountReais + 1, "category": "aluguel", "is_pending": false,
	})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected an error for an amount over the cap")
	}
}

func TestCreateEntryToolRejectsInvalidType(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	raw, _ := json.Marshal(map[string]any{
		"type": "transfer", "amount": 100.0, "category": "aluguel", "is_pending": false,
	})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected an error for an invalid type")
	}
}

func TestCreateEntryToolCoercesUnknownCategory(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	callTool(t, h, "u1", map[string]any{
		"type": "income", "amount": 100.0, "category": "criptomoedas", "is_pending": false,
	})

	entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	// An out-of-set category falls back to the income default, not persisted verbatim.
	if entries[0].Category != "outros_receitas" {
		t.Fatalf("expected coerced category outros_receitas, got %q", entries[0].Category)
	}
}

func TestCreateEntryToolIgnoresDueDateWhenNotPending(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	callTool(t, h, "u1", map[string]any{
		"type": "expense", "amount": 100.0, "category": "aluguel",
		"due_date": "2026-08-20", "is_pending": false,
	})

	entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].DueDate != nil {
		t.Fatalf("expected no due date on a settled entry, got %+v", entries[0].DueDate)
	}
}

func TestMonthSummaryToolReturnsReais(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	month := "2026-07"
	seed := func(entryID string, amount int64, typ domain.EntryType) {
		if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
			UserID: "u1", EntryID: entryID, Date: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
			Amount: amount, Type: typ, PaymentStatus: domain.PaymentStatusPaid,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed("a", 90000, domain.EntryTypeIncome)
	seed("b", 25000, domain.EntryTypeExpense)

	h := handlerFor(t, store, "get_month_summary")
	out := callTool(t, h, "u1", map[string]any{"month": month})

	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out)
	}
	if m["income"] != 900.0 || m["expense"] != 250.0 || m["balance"] != 650.0 {
		t.Fatalf("unexpected summary: %+v", m)
	}
}

func TestSearchEntriesToolFiltersByDescription(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	save := func(id, desc string) {
		if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
			UserID: "u1", EntryID: id, Date: time.Now().UTC(), Amount: 1000,
			Category: "outros_despesas", Type: domain.EntryTypeExpense,
			Description: desc, PaymentStatus: domain.PaymentStatusPaid,
		}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	save("1", "Aluguel da loja")
	save("2", "Conta de luz")

	h := handlerFor(t, store, "search_entries")
	// Case-insensitive substring match on the in-memory store.
	out := callTool(t, h, "u1", map[string]any{"query": "aluguel"})

	results, ok := out.([]map[string]any)
	if !ok {
		t.Fatalf("expected slice result, got %T", out)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(results), results)
	}
	if results[0]["description"] != "Aluguel da loja" {
		t.Fatalf("unexpected match: %+v", results[0])
	}
}

func TestListDueEntriesToolDefaultsToPending(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	due := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: "p", Date: time.Now().UTC(), Amount: 5000,
		Category: "aluguel", Type: domain.EntryTypeExpense, DueDate: &due,
		PaymentStatus: domain.PaymentStatusPending,
	}); err != nil {
		t.Fatalf("save pending: %v", err)
	}
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: "paid", Date: time.Now().UTC(), Amount: 5000,
		Category: "aluguel", Type: domain.EntryTypeExpense,
		PaymentStatus: domain.PaymentStatusPaid,
	}); err != nil {
		t.Fatalf("save paid: %v", err)
	}

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{})

	results, ok := out.([]map[string]any)
	if !ok {
		t.Fatalf("expected slice result, got %T", out)
	}
	if len(results) != 1 || results[0]["status"] != string(domain.PaymentStatusPending) {
		t.Fatalf("expected only the pending entry, got %+v", results)
	}
}

func TestEditEntryToolUpdatesFields(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: "e1", Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Amount: 1000, Category: "outros_despesas", Type: domain.EntryTypeExpense,
		Description: "old", PaymentStatus: domain.PaymentStatusPaid,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := handlerFor(t, store, "edit_financial_entry")
	callTool(t, h, "u1", map[string]any{
		"entry_id":    "e1",
		"amount":      50.0,
		"category":    "aluguel",
		"description": "new",
	})

	entry, err := store.GetEntry(context.Background(), "u1", "e1")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Amount != 5000 || entry.Category != "aluguel" || entry.Description != "new" {
		t.Fatalf("unexpected entry after edit: %+v", entry)
	}
}

func TestEditEntryToolMarkingPaidSetsPaymentDate(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	due := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: "e1", Date: time.Now().UTC(), Amount: 5000,
		Category: "aluguel", Type: domain.EntryTypeExpense, DueDate: &due,
		PaymentStatus: domain.PaymentStatusPending,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := handlerFor(t, store, "edit_financial_entry")
	callTool(t, h, "u1", map[string]any{"entry_id": "e1", "is_pending": false})

	entry, err := store.GetEntry(context.Background(), "u1", "e1")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.PaymentStatus != domain.PaymentStatusPaid {
		t.Fatalf("expected paid status, got %s", entry.PaymentStatus)
	}
	if entry.PaymentDate == nil {
		t.Fatal("expected PaymentDate to be set")
	}
}

func TestEditEntryToolUnknownEntryReturnsError(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "edit_financial_entry")

	raw, _ := json.Marshal(map[string]any{"entry_id": "missing", "amount": 10.0})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected an error for an unknown entry_id")
	}
}

func TestEditEntryToolRejectsAmountOverCap(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: "e1", Date: time.Now().UTC(), Amount: 1000,
		Category: "aluguel", Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := handlerFor(t, store, "edit_financial_entry")
	raw, _ := json.Marshal(map[string]any{"entry_id": "e1", "amount": maxEntryAmountReais + 1})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected an error for an amount over the cap")
	}

	entry, err := store.GetEntry(context.Background(), "u1", "e1")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Amount != 1000 {
		t.Fatalf("expected amount unchanged after rejected edit, got %d", entry.Amount)
	}
}
