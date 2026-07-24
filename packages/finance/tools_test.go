package finance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func handlerFor(t *testing.T, store Store, name string) ToolFunc {
	t.Helper()
	for _, tool := range FinanceTools(store, "") {
		if tool.Name == name {
			return tool.Handler
		}
	}
	t.Fatalf("tool %q not registered", name)
	return nil
}

func callTool(t *testing.T, h ToolFunc, userID string, args map[string]any) any {
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
	wantDue := domain.NewCalendarDate(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	if e.DueDate == nil || !e.DueDate.Equal(wantDue) {
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

func TestResumoMensalToolReturnsIncomeExpenseBalanceAndGoal(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	month := "2026-07"
	seed := func(id string, amount int64, typ domain.EntryType) {
		cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
		if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
			UserID: "u1", EntryID: domain.EntryID(id), TransactionDate: cd,
			Amount: amount, Type: typ, PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate: &cd, Source: domain.SourceManual,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed("a", 90000, domain.EntryTypeIncome)
	seed("b", 25000, domain.EntryTypeExpense)

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})

	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out)
	}
	if m["income"] != 900.0 || m["expense"] != 250.0 || m["balance"] != 650.0 {
		t.Fatalf("unexpected summary: %+v", m)
	}
	if m["goal"] != nil {
		t.Fatalf("expected goal to be nil, got %+v", m["goal"])
	}
}

func TestSearchEntriesToolFiltersByDescription(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	save := func(id, desc string) {
		cd := domain.NewCalendarDate(time.Now().UTC())
		if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
			UserID: "u1", EntryID: domain.EntryID(id), TransactionDate: cd,
			Amount: 1000, Category: "outros_despesas", Type: domain.EntryTypeExpense,
			Description: desc, PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate: &cd, Source: domain.SourceManual,
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
	due := domain.NewCalendarDate(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC))
	now := domain.NewCalendarDate(time.Now().UTC())
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("p"), TransactionDate: now, Amount: 5000,
		Category: "aluguel", Type: domain.EntryTypeExpense, DueDate: &due,
		PaymentStatus: domain.PaymentStatusPending, Source: domain.SourceManual,
	}); err != nil {
		t.Fatalf("save pending: %v", err)
	}
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("paid"), TransactionDate: now, Amount: 5000,
		Category: "aluguel", Type: domain.EntryTypeExpense,
		PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &now, Source: domain.SourceManual,
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
	cd := domain.NewCalendarDate(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("e1"), TransactionDate: cd,
		Amount: 1000, Category: "outros_despesas", Type: domain.EntryTypeExpense,
		Description: "old", PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate: &cd, Source: domain.SourceManual,
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
	due := domain.NewCalendarDate(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC))
	now := domain.NewCalendarDate(time.Now().UTC())
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("e1"), TransactionDate: now, Amount: 5000,
		Category: "aluguel", Type: domain.EntryTypeExpense, DueDate: &due,
		PaymentStatus: domain.PaymentStatusPending, Source: domain.SourceManual,
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
	cd := domain.NewCalendarDate(time.Now().UTC())
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("e1"), TransactionDate: cd, Amount: 1000,
		Category: "aluguel", Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate: &cd, Source: domain.SourceManual,
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

func TestResumoMensalToolComMetaIncluiProgresso(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	month := "2026-07"

	// Seed entries: R$ 500 income, R$ 200 expense
	cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	for _, e := range []domain.FinancialEntry{
		{UserID: "u1", EntryID: domain.EntryID("inc1"), TransactionDate: cd, Amount: 50000, Type: domain.EntryTypeIncome, Category: "venda_balcao", PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &cd, Source: domain.SourceManual},
		{UserID: "u1", EntryID: domain.EntryID("exp1"), TransactionDate: cd, Amount: 20000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &cd, Source: domain.SourceManual},
	} {
		if err := store.SaveEntry(ctx, e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Seed goal: R$ 1000 revenue target, R$ 500 expense ceiling
	goal := domain.Goal{UserID: "u1", Month: month, RevenueTarget: 100000, ExpenseTarget: 50000}
	if err := store.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})

	m := out.(map[string]any)
	g := m["goal"].(map[string]any)

	if g["revenue_target"] != 1000.0 {
		t.Fatalf("expected revenue_target 1000, got %v", g["revenue_target"])
	}
	if g["revenue_progress_pct"] != 50.0 {
		t.Fatalf("expected revenue_progress_pct 50, got %v", g["revenue_progress_pct"])
	}
	if g["expense_target"] != 500.0 {
		t.Fatalf("expected expense_target 500, got %v", g["expense_target"])
	}
	if g["expense_progress_pct"] != 40.0 {
		t.Fatalf("expected expense_progress_pct 40, got %v", g["expense_progress_pct"])
	}
}

func TestResumoMensalToolSemMetaRetornaGoalNil(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	month := "2026-07"

	cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("e1"), TransactionDate: cd,
		Amount: 1000, Type: domain.EntryTypeIncome, PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate: &cd, Source: domain.SourceManual,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})

	m := out.(map[string]any)
	if m["goal"] != nil {
		t.Fatalf("expected goal to be nil, got %+v", m["goal"])
	}
}

func TestDefinirMetaPersisteRevenueTarget(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "definir_meta")

	out := callTool(t, h, "u1", map[string]any{
		"month":            "2026-08",
		"meta_faturamento": 50000.0,
	})

	m := out.(map[string]any)
	if m["meta_faturamento"] != 50000.0 {
		t.Fatalf("expected meta_faturamento 50000, got %v", m["meta_faturamento"])
	}

	goal, err := store.GetGoal(context.Background(), "u1", "2026-08")
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.RevenueTarget != reaisToCentavos(50000.0) {
		t.Fatalf("expected RevenueTarget %d, got %d", reaisToCentavos(50000.0), goal.RevenueTarget)
	}
}

func TestDefinirMetaPersisteExpenseTarget(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "definir_meta")

	out := callTool(t, h, "u1", map[string]any{
		"month":         "2026-08",
		"teto_despesas": 30000.0,
	})

	m := out.(map[string]any)
	if m["teto_despesas"] != 30000.0 {
		t.Fatalf("expected teto_despesas 30000, got %v", m["teto_despesas"])
	}

	goal, err := store.GetGoal(context.Background(), "u1", "2026-08")
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.ExpenseTarget != reaisToCentavos(30000.0) {
		t.Fatalf("expected ExpenseTarget %d, got %d", reaisToCentavos(30000.0), goal.ExpenseTarget)
	}
}

func TestDefinirMetaRejeitaSemTargets(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "definir_meta")

	raw, _ := json.Marshal(map[string]any{"month": "2026-08"})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected error when no targets provided")
	}
}

func TestDefinirMetaMergeComExisting(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	// Pre-save a goal with only revenue target
	if err := store.SaveGoal(ctx, domain.Goal{UserID: "u1", Month: "2026-09", RevenueTarget: 100000}); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	h := handlerFor(t, store, "definir_meta")
	out := callTool(t, h, "u1", map[string]any{
		"month":         "2026-09",
		"teto_despesas": 40000.0,
	})

	m := out.(map[string]any)
	if m["meta_faturamento"] != 1000.0 {
		t.Fatalf("expected existing meta_faturamento 1000 preserved, got %v", m["meta_faturamento"])
	}
	if m["teto_despesas"] != 40000.0 {
		t.Fatalf("expected teto_despesas 40000, got %v", m["teto_despesas"])
	}
}

func TestDefinirMetaDefaultsToCurrentMonth(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "definir_meta")

	now := time.Now().UTC()
	expectedMonth := now.Format("2006-01")

	out := callTool(t, h, "u1", map[string]any{
		"meta_faturamento": 1000.0,
	})

	m := out.(map[string]any)
	if m["month"] != expectedMonth {
		t.Fatalf("expected month %q, got %q", expectedMonth, m["month"])
	}
}

func TestResumoMensalRevenueCappedAt100WhenExceedsTarget(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	month := "2026-07"

	cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err := store.SaveEntry(ctx, domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("inc1"), TransactionDate: cd,
		Amount: 200000, Type: domain.EntryTypeIncome, Category: "venda_balcao", PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate: &cd, Source: domain.SourceManual,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	goal := domain.Goal{UserID: "u1", Month: month, RevenueTarget: 100000}
	if err := store.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})
	g := out.(map[string]any)["goal"].(map[string]any)

	if g["revenue_progress_pct"] != 100.0 {
		t.Fatalf("expected revenue_progress_pct capped at 100, got %v", g["revenue_progress_pct"])
	}
}

func TestResumoMensalExpenseCappedAt100WhenExceedsTarget(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	month := "2026-07"

	cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err := store.SaveEntry(ctx, domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("exp1"), TransactionDate: cd,
		Amount: 60000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate: &cd, Source: domain.SourceManual,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	goal := domain.Goal{UserID: "u1", Month: month, RevenueTarget: 100000, ExpenseTarget: 50000}
	if err := store.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})
	g := out.(map[string]any)["goal"].(map[string]any)

	if g["expense_progress_pct"] != 100.0 {
		t.Fatalf("expected expense_progress_pct capped at 100, got %v", g["expense_progress_pct"])
	}
}

func TestResumoMensalOnlyExpenseTargetShowsGoalBlock(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	month := "2026-07"

	cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err := store.SaveEntry(ctx, domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("exp1"), TransactionDate: cd,
		Amount: 20000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate: &cd, Source: domain.SourceManual,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Only expense target set, no revenue target
	goal := domain.Goal{UserID: "u1", Month: month, ExpenseTarget: 50000}
	if err := store.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})
	g := out.(map[string]any)["goal"].(map[string]any)

	if g["expense_target"] != 500.0 {
		t.Fatalf("expected expense_target 500, got %v", g["expense_target"])
	}
	if g["expense_progress_pct"] != 40.0 {
		t.Fatalf("expected expense_progress_pct 40, got %v", g["expense_progress_pct"])
	}
	if _, ok := g["revenue_progress_pct"]; ok {
		t.Fatal("expected no revenue_progress_pct when revenue target is 0")
	}
}

func TestResumoMensalDefaultsToCurrentMonth(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	expectedMonth := now.Format("2006-01")

	cd := domain.NewCalendarDate(now)
	if err := store.SaveEntry(ctx, domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("inc1"), TransactionDate: cd,
		Amount: 50000, Type: domain.EntryTypeIncome, PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate: &cd, Source: domain.SourceManual,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	goal := domain.Goal{UserID: "u1", Month: expectedMonth, RevenueTarget: 100000}
	if err := store.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{})

	m := out.(map[string]any)
	if m["month"] != expectedMonth {
		t.Fatalf("expected month %q, got %q", expectedMonth, m["month"])
	}
}

func TestDefinirMetaAmbosTargets(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "definir_meta")

	out := callTool(t, h, "u1", map[string]any{
		"month":            "2026-10",
		"meta_faturamento": 80000.0,
		"teto_despesas":    60000.0,
	})

	m := out.(map[string]any)
	if m["meta_faturamento"] != 80000.0 {
		t.Fatalf("expected meta_faturamento 80000, got %v", m["meta_faturamento"])
	}
	if m["teto_despesas"] != 60000.0 {
		t.Fatalf("expected teto_despesas 60000, got %v", m["teto_despesas"])
	}
}

func TestDefinirMetaRejeitaValorZerado(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "definir_meta")

	raw, _ := json.Marshal(map[string]any{
		"month":            "2026-08",
		"meta_faturamento": 0.0,
		"teto_despesas":    0.0,
	})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected error when both targets are zero")
	}
}

func TestSearchEntriesByDescription(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	cd := domain.NewCalendarDate(now)

	entries := []domain.FinancialEntry{
		{
			UserID: "ledger", EntryID: domain.EntryID("e1"),
			TransactionDate: cd, Amount: 350000,
			Category: "aluguel", Type: domain.EntryTypeExpense,
			Description:   "Aluguel da Loja - Matriz",
			PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate:   &cd, Source: domain.SourceManual,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			UserID: "ledger", EntryID: domain.EntryID("e2"),
			TransactionDate: cd, Amount: 1200000,
			Category: "folha_pagamento", Type: domain.EntryTypeExpense,
			Description:   "Folha de Pagamento",
			PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate:   &cd, Source: domain.SourceManual,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, e := range entries {
		if err := store.SaveEntry(ctx, e); err != nil {
			t.Fatalf("save entry: %v", err)
		}
	}

	h := handlerFor(t, store, "search_entries")
	out := callTool(t, h, "ledger", map[string]any{"query": "aluguel"})

	results, ok := out.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", out)
	}
	if len(results) == 0 {
		t.Fatal("search_entries returned 0 results for query 'aluguel'")
	}
	if results[0]["description"] != "Aluguel da Loja - Matriz" {
		t.Fatalf("expected 'Aluguel da Loja - Matriz', got %v", results[0]["description"])
	}
	if results[0]["amount"] != 3500.00 {
		t.Fatalf("expected amount 3500.00, got %v", results[0]["amount"])
	}

	// Without query returns all entries.
	outAll := callTool(t, h, "ledger", map[string]any{})
	all, ok := outAll.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", outAll)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries with no query filter, got %d", len(all))
	}
}

func TestSearchEntriesByDescriptionCaseInsensitive(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	cd := domain.NewCalendarDate(now)

	if err := store.SaveEntry(ctx, domain.FinancialEntry{
		UserID: "ledger", EntryID: domain.EntryID("e1"),
		TransactionDate: cd, Amount: 350000,
		Category: "aluguel", Type: domain.EntryTypeExpense,
		Description:   "Aluguel da Loja - Matriz",
		PaymentStatus: domain.PaymentStatusPaid,
		PaymentDate:   &cd, Source: domain.SourceManual,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save entry: %v", err)
	}

	h := handlerFor(t, store, "search_entries")
	out := callTool(t, h, "ledger", map[string]any{"query": "ALUGUEL"})

	results, ok := out.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", out)
	}
	if len(results) == 0 {
		t.Fatal("search_entries returned 0 results for query 'ALUGUEL' (upper)")
	}
}

func TestSearchEntriesByCategory(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	cd := domain.NewCalendarDate(now)

	entries := []domain.FinancialEntry{
		{
			UserID: "ledger", EntryID: domain.EntryID("e1"),
			TransactionDate: cd, Amount: 350000,
			Category: "aluguel", Type: domain.EntryTypeExpense,
			Description:   "Aluguel",
			PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate:   &cd, Source: domain.SourceManual,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			UserID: "ledger", EntryID: domain.EntryID("e2"),
			TransactionDate: cd, Amount: 50000,
			Category: "energia_agua", Type: domain.EntryTypeExpense,
			Description:   "Conta de Luz",
			PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate:   &cd, Source: domain.SourceManual,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, e := range entries {
		if err := store.SaveEntry(ctx, e); err != nil {
			t.Fatalf("save entry: %v", err)
		}
	}

	h := handlerFor(t, store, "search_entries")
	out := callTool(t, h, "ledger", map[string]any{"category": "aluguel"})

	results, ok := out.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", out)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for category 'aluguel', got %d", len(results))
	}
}

func TestSearchEntriesByPeriod(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	lastMonth := now.AddDate(0, -1, 0)
	cdNow := domain.NewCalendarDate(now)
	cdLast := domain.NewCalendarDate(lastMonth)

	entries := []domain.FinancialEntry{
		{
			UserID: "ledger", EntryID: domain.EntryID("e1"),
			TransactionDate: cdNow, Amount: 350000,
			Category: "aluguel", Type: domain.EntryTypeExpense,
			Description:   "Aluguel deste mês",
			PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate:   &cdNow, Source: domain.SourceManual,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			UserID: "ledger", EntryID: domain.EntryID("e2"),
			TransactionDate: cdLast, Amount: 350000,
			Category: "aluguel", Type: domain.EntryTypeExpense,
			Description:   "Aluguel mês passado",
			PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate:   &cdLast, Source: domain.SourceManual,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, e := range entries {
		if err := store.SaveEntry(ctx, e); err != nil {
			t.Fatalf("save entry: %v", err)
		}
	}

	h := handlerFor(t, store, "search_entries")

	from := time.Date(lastMonth.Year(), lastMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(lastMonth.Year(), lastMonth.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	out := callTool(t, h, "ledger", map[string]any{
		"from": from.Format("2006-01-02"),
		"to":   to.Format("2006-01-02"),
	})

	results, ok := out.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", out)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for last month, got %d", len(results))
	}
	if results[0]["description"] != "Aluguel mês passado" {
		t.Fatalf("expected 'Aluguel mês passado', got %v", results[0]["description"])
	}
}
