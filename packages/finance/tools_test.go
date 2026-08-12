package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func handlerFor(t *testing.T, store Store, name string) ToolFunc {
	t.Helper()
	return handlerForIn(t, store, name, time.UTC)
}

func handlerForIn(t *testing.T, store Store, name string, loc *time.Location) ToolFunc {
	t.Helper()
	for _, tool := range FinanceTools(store, "", loc) {
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

// entriesOf unwraps the listing envelope the list/search tools return. The
// envelope carries the totals and the truncation flag alongside the rows; a
// test that only cares about the rows reaches through it here.
func entriesOf(t *testing.T, out any) []map[string]any {
	t.Helper()
	env, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected listing envelope, got %T", out)
	}
	rows, ok := env["entries"].([]map[string]any)
	if !ok {
		t.Fatalf("expected entries slice, got %T", env["entries"])
	}
	return rows
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

func TestToolsRecordThePaymentMethod(t *testing.T) {
	t.Parallel()

	t.Run("created already settled, as the user said it", func(t *testing.T) {
		store := NewInMemoryStore()
		h := handlerFor(t, store, "create_financial_entry")

		callTool(t, h, "u1", map[string]any{
			"type":            "expense",
			"amount":          120.0,
			"category":        "fornecedor_geral",
			"description":     "Fornecedor",
			"is_pending":      false,
			"forma_pagamento": " pix ",
		})

		entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
		if len(entries) != 1 || entries[0].PaymentMethod != "pix" {
			t.Fatalf("payment method = %q, want %q", entries[0].PaymentMethod, "pix")
		}
	})

	// "Vou pagar amanhã no pix" is not a payment. A bill nobody has settled
	// cannot say how it was settled.
	t.Run("dropped on a pending entry", func(t *testing.T) {
		store := NewInMemoryStore()
		h := handlerFor(t, store, "create_financial_entry")

		callTool(t, h, "u1", map[string]any{
			"type":            "expense",
			"amount":          300.0,
			"category":        "energia_agua",
			"due_date":        "2026-08-20",
			"is_pending":      true,
			"forma_pagamento": "pix",
		})

		entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
		if len(entries) != 1 || entries[0].PaymentMethod != "" {
			t.Fatalf("payment method on a pending entry = %q, want empty", entries[0].PaymentMethod)
		}
	})

	// The message that quits a bill usually says how: "paguei o fornecedor no
	// pix" is one edit, not two.
	t.Run("recorded while quitting a pending bill", func(t *testing.T) {
		store := NewInMemoryStore()
		seedForEdit(t, store, "u1", "e1")
		h := handlerFor(t, store, "edit_financial_entry")

		callTool(t, h, "u1", map[string]any{
			"entry_id":        "e1",
			"is_pending":      false,
			"forma_pagamento": "dinheiro",
		})

		e, err := store.FindEntryByID(context.Background(), "u1", "e1")
		if err != nil {
			t.Fatalf("find entry: %v", err)
		}
		if e.PaymentStatus != domain.PaymentStatusPaid || e.PaymentMethod != "dinheiro" {
			t.Fatalf("entry = %+v, want paid in dinheiro", e)
		}
	})

	t.Run("dropped when the entry is reopened", func(t *testing.T) {
		store := NewInMemoryStore()
		seedForEdit(t, store, "u1", "e1")
		h := handlerFor(t, store, "edit_financial_entry")

		callTool(t, h, "u1", map[string]any{"entry_id": "e1", "is_pending": false, "forma_pagamento": "pix"})
		callTool(t, h, "u1", map[string]any{"entry_id": "e1", "is_pending": true})

		e, _ := store.FindEntryByID(context.Background(), "u1", "e1")
		if e.PaymentMethod != "" {
			t.Fatalf("payment method after reopening = %q, want empty", e.PaymentMethod)
		}
	})

	t.Run("a hallucinated essay is refused, not stored", func(t *testing.T) {
		store := NewInMemoryStore()
		h := handlerFor(t, store, "create_financial_entry")

		raw, _ := json.Marshal(map[string]any{
			"type": "expense", "amount": 10.0, "category": "fornecedor_geral",
			"is_pending":      false,
			"forma_pagamento": strings.Repeat("a", domain.MaxPaymentMethodLen+1),
		})
		if _, err := h(context.Background(), "u1", raw); err == nil {
			t.Fatal("expected an error for an over-long forma_pagamento")
		}
		entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
		if len(entries) != 0 {
			t.Fatalf("stored %d entries, want none", len(entries))
		}
	})
}

// seedForEdit puts one pending expense in the store for the edit tool to act
// on, addressed by the id the tool looks it up by.
func seedForEdit(t *testing.T, store Store, userID, id string) {
	t.Helper()
	e := domain.FinancialEntry{
		UserID:          userID,
		EntryID:         domain.EntryID(id),
		TransactionDate: domain.NewCalendarDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
		Amount:          15000,
		Category:        "fornecedor_geral",
		Description:     "Fornecedor",
		Type:            domain.EntryTypeExpense,
		PaymentStatus:   domain.PaymentStatusPending,
		Source:          domain.SourceWhatsApp,
	}
	if err := store.SaveEntry(context.Background(), e); err != nil {
		t.Fatalf("seed entry: %v", err)
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

// A category nobody defined is a question for the model, not a bucket to dump
// the entry in. It used to be silently filed under "Outros (Receita)", which
// was defensible while the category set was a closed enum in the schema — the
// slug could only be a hallucination. Now that the catalog is the user's to
// extend, the same slug is just as likely to be the "venda_varejo" they asked
// for a message ago, and answering "lançado" while burying it in Outros is how
// atacado and varejo end up in one bucket with nothing to show for it.
func TestCreateEntryToolRefusesUnknownCategory(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, "create_financial_entry")

	raw, _ := json.Marshal(map[string]any{
		"type": "income", "amount": 100.0, "category": "criptomoedas", "is_pending": false,
	})
	_, err := h(context.Background(), "u1", raw)
	if err == nil {
		t.Fatal("expected an unknown category to be refused")
	}
	// The refusal has to be actionable: the model gets the categories that would
	// have worked, and where to go if the user really meant a new one.
	if !strings.Contains(err.Error(), "venda_balcao") || !strings.Contains(err.Error(), createCategoryToolName) {
		t.Errorf("error = %q, want the valid slugs and %s", err, createCategoryToolName)
	}

	entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
	if len(entries) != 0 {
		t.Fatalf("expected nothing written, got %d entries", len(entries))
	}
}

// The point of the whole change: a category the user created from WhatsApp is
// a category they can file a sale under, one message later.
func TestCreateEntryToolAcceptsAUserCreatedCategory(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	create := handlerFor(t, store, createCategoryToolName)
	callTool(t, create, "u1", map[string]any{"label": "Venda Varejo", "type": "income"})

	out := callTool(t, handlerFor(t, store, "create_financial_entry"), "u1", map[string]any{
		"type": "income", "amount": 250.0, "category": "venda_varejo", "is_pending": false,
	})
	if got := out.(map[string]any)["category_label"]; got != "Venda Varejo" {
		t.Errorf("category_label = %v, want the label the user gave it", got)
	}

	entries, _ := store.ListEntries(context.Background(), "u1", EntryFilter{})
	if len(entries) != 1 || entries[0].Category != "venda_varejo" {
		t.Fatalf("entries = %+v, want one filed under venda_varejo", entries)
	}
	// A new income category does not invent a new kind of money: the origin is
	// what makes this faturamento, and it is still "venda" (ADR-016).
	if entries[0].Origin != domain.OriginVenda {
		t.Errorf("Origin = %q, want venda", entries[0].Origin)
	}
}

// Filing a sale under an expense category is refused for the same reason an
// unknown slug is: it is a breakdown that lies later.
func TestCreateEntryToolRefusesACategoryOfTheOtherType(t *testing.T) {
	t.Parallel()

	h := handlerFor(t, NewInMemoryStore(), "create_financial_entry")
	raw, _ := json.Marshal(map[string]any{
		"type": "income", "amount": 100.0, "category": "aluguel", "is_pending": false,
	})
	if _, err := h(context.Background(), "u1", raw); err == nil {
		t.Fatal("expected an income entry under an expense category to be refused")
	}
}

func TestCreateCategoryTool(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, createCategoryToolName)

	out := callTool(t, h, "u1", map[string]any{"label": "Venda Varejo", "type": "income"}).(map[string]any)
	if out["status"] != "created" || out["slug"] != "venda_varejo" {
		t.Fatalf("result = %+v, want a created venda_varejo", out)
	}

	stored, err := store.ListCategories(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored = %+v, want exactly the one category", stored)
	}
	// Same shape as one created in the dashboard, and not a default: the
	// defaults live in the codebase.
	if stored[0].Slug != "venda_varejo" || stored[0].Label != "Venda Varejo" {
		t.Errorf("stored = %+v, want the normalized slug and the label as typed", stored[0])
	}
	if stored[0].Type != domain.EntryTypeIncome || stored[0].Default {
		t.Errorf("stored = %+v, want an income category that is not a default", stored[0])
	}
}

// Creating one that is already there reports it instead of writing over it.
// SaveCategory is a PutItem: re-creating "Venda Atacado" after the user renamed
// it would silently rename it back.
func TestCreateCategoryToolDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, createCategoryToolName)
	callTool(t, h, "u1", map[string]any{"label": "Atacado (CNPJ)", "type": "income"})

	out := callTool(t, h, "u1", map[string]any{"label": "Atacado CNPJ", "type": "income"}).(map[string]any)
	if out["status"] != "ja_existe" {
		t.Fatalf("result = %+v, want it reported as already existing", out)
	}
	if out["label"] != "Atacado (CNPJ)" {
		t.Errorf("label = %v, want the label already stored", out["label"])
	}

	stored, _ := store.ListCategories(context.Background(), "u1")
	if len(stored) != 1 {
		t.Fatalf("stored %d categories, want the duplicate refused", len(stored))
	}
}

// A default is already there for everyone, seeded or not, so creating one is
// the same non-event as creating a duplicate — never a second definition.
func TestCreateCategoryToolWillNotShadowADefault(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	h := handlerFor(t, store, createCategoryToolName)

	out := callTool(t, h, "u1", map[string]any{"label": "Venda Balcão", "type": "income"}).(map[string]any)
	if out["status"] != "ja_existe" || out["slug"] != "venda_balcao" {
		t.Fatalf("result = %+v, want the default reported as existing", out)
	}
	if stored, _ := store.ListCategories(context.Background(), "u1"); len(stored) != 0 {
		t.Fatalf("stored = %+v, want nothing written over the default", stored)
	}
}

func TestCreateCategoryToolRejectsBadInput(t *testing.T) {
	t.Parallel()

	h := handlerFor(t, NewInMemoryStore(), createCategoryToolName)
	for _, args := range []map[string]any{
		{"label": "   ", "type": "income"},
		{"label": "!!!", "type": "income"},
		{"label": "Venda Atacado", "type": "outro"},
		{"label": "Venda Atacado"},
	} {
		raw, _ := json.Marshal(args)
		if _, err := h(context.Background(), "u1", raw); err == nil {
			t.Errorf("args %v: expected an error, got none", args)
		}
	}
}

func TestListCategoriesTool(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	callTool(t, handlerFor(t, store, createCategoryToolName), "u1",
		map[string]any{"label": "Venda Varejo", "type": "income"})

	out := callTool(t, handlerFor(t, store, listCategoriesToolName), "u1",
		map[string]any{"type": "income"}).(map[string]any)

	cats, ok := out["categorias"].([]map[string]any)
	if !ok {
		t.Fatalf("categorias = %T, want a list", out["categorias"])
	}
	var sawDefault, sawCustom bool
	for _, c := range cats {
		if c["tipo"] != "income" {
			t.Errorf("category %v leaked past the type filter", c)
		}
		switch c["slug"] {
		case "venda_balcao":
			sawDefault = true
		case "venda_varejo":
			sawCustom = true
		}
	}
	if !sawDefault || !sawCustom {
		t.Errorf("categorias = %+v, want the defaults and the user's own", cats)
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

// The tool reports faturamento and entradas de caixa separately, so the model
// can never present a loan as a sale. The loan seeded here is the difference
// between the two numbers.
func TestResumoMensalToolSeparatesRevenueFromCashIn(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	month := "2026-07"
	seed := func(id string, amount int64, typ domain.EntryType, origin domain.IncomeOrigin) {
		cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
		if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
			UserID: "u1", EntryID: domain.EntryID(id), TransactionDate: cd,
			Amount: amount, Type: typ, PaymentStatus: domain.PaymentStatusPaid,
			PaymentDate: &cd, Source: domain.SourceManual, Origin: origin,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seed("a", 90000, domain.EntryTypeIncome, domain.OriginVenda)
	seed("loan", 500000, domain.EntryTypeIncome, domain.OriginEmprestimo)
	seed("b", 25000, domain.EntryTypeExpense, "")

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})

	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out)
	}
	if m["faturamento"] != 900.0 {
		t.Errorf("faturamento = %v, want 900 — the loan is not a sale", m["faturamento"])
	}
	if m["entradas_de_caixa"] != 5900.0 {
		t.Errorf("entradas_de_caixa = %v, want 5900 — the loan is real money", m["entradas_de_caixa"])
	}
	if m["expense"] != 250.0 || m["balance"] != 5650.0 {
		t.Fatalf("unexpected summary: %+v", m)
	}
	if m["goal"] != nil {
		t.Fatalf("expected goal to be nil, got %+v", m["goal"])
	}
}

// A loan must not be reachable as a sale through the create tool either: the
// origin the model picks is what decides, and an omitted one means a sale.
func TestCreateEntryToolRecordsOrigin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args map[string]any
		want domain.IncomeOrigin
	}{
		{"explicit loan", map[string]any{"origem": "emprestimo"}, domain.OriginEmprestimo},
		{"omitted defaults to a sale", map[string]any{}, domain.OriginVenda},
		{"hallucinated value falls back to outros", map[string]any{"origem": "dinheiro_magico"}, domain.OriginOutros},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemoryStore()
			h := handlerFor(t, store, "create_financial_entry")
			args := map[string]any{
				"type": "income", "amount": 100.0,
				"category": "venda_balcao", "is_pending": false,
			}
			maps.Copy(args, tc.args)
			callTool(t, h, "u1", args)

			entries, err := store.ListEntries(context.Background(), "u1", EntryFilter{})
			if err != nil || len(entries) != 1 {
				t.Fatalf("list entries: %v (got %d)", err, len(entries))
			}
			if entries[0].Origin != tc.want {
				t.Fatalf("origin = %q, want %q", entries[0].Origin, tc.want)
			}
		})
	}
}

// recebimento_cliente is deliberately absent from the create tool: settling a
// crediário is an edit to the existing pending sale, and offering the model an
// origin that sounds like "customer paid me" makes it create a second entry for
// a sale that is already in the ledger.
func TestCreateEntryToolDoesNotOfferRecebimentoCliente(t *testing.T) {
	t.Parallel()

	for _, slug := range createOriginSlugs() {
		if slug == string(domain.OriginRecebimentoCliente) {
			t.Fatalf("create_financial_entry offers %q, which double-counts a crediário sale", slug)
		}
	}
	if len(createOriginSlugs()) != len(domain.IncomeOrigins())-1 {
		t.Fatalf("expected exactly one origin to be withheld from the create tool")
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

	results := entriesOf(t, out)
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

	results := entriesOf(t, out)
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

	entry, err := store.FindEntryByID(context.Background(), "u1", "e1")
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

	entry, err := store.FindEntryByID(context.Background(), "u1", "e1")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.PaymentStatus != domain.PaymentStatusPaid {
		t.Fatalf("expected paid status, got %s", entry.PaymentStatus)
	}
	if entry.PaymentDate == nil {
		t.Fatal("expected PaymentDate to be set")
	}
	if got, want := entry.PaymentDate.String(), time.Now().UTC().Format("2006-01-02"); got != want {
		t.Fatalf("payment date = %s, want today (%s)", got, want)
	}
}

func TestEditEntryToolPaysOnThePharmacysCalendarDay(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	day := domain.NewCalendarDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
	if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("e1"), TransactionDate: day, Amount: 5000,
		Category: "aluguel", Type: domain.EntryTypeExpense,
		PaymentStatus: domain.PaymentStatusPending, Source: domain.SourceManual,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Far enough east that its calendar day differs from UTC's for part of the
	// day — settling in UTC recorded tomorrow for every evening in Brazil.
	loc := time.FixedZone("UTC+14", 14*3600)
	h := handlerForIn(t, store, "edit_financial_entry", loc)
	callTool(t, h, "u1", map[string]any{"entry_id": "e1", "is_pending": false})

	entry, err := store.FindEntryByID(context.Background(), "u1", "e1")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got, want := entry.PaymentDate.String(), time.Now().In(loc).Format("2006-01-02"); got != want {
		t.Fatalf("payment date = %s, want %s (the configured zone's today)", got, want)
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

	entry, err := store.FindEntryByID(context.Background(), "u1", "e1")
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry.Amount != 1000 {
		t.Fatalf("expected amount unchanged after rejected edit, got %d", entry.Amount)
	}
}

// End to end over the tools, which is where the two halves of ADR-024 meet: a
// category created in a chat, a sale filed under it, and the month's faturamento
// coming back split by kind of sale.
func TestResumoMensalToolBreaksFaturamentoByCategory(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	callTool(t, handlerFor(t, store, createCategoryToolName), "u1",
		map[string]any{"label": "Venda Varejo", "type": "income"})

	create := handlerFor(t, store, "create_financial_entry")
	callTool(t, create, "u1", map[string]any{
		"type": "income", "amount": 600.0, "category": "venda_varejo",
		"date": "2026-07-05", "is_pending": false,
	})
	callTool(t, create, "u1", map[string]any{
		"type": "income", "amount": 400.0, "category": "venda_atacado",
		"date": "2026-07-06", "is_pending": false,
	})
	// A loan is money in and is not a sale, so it is in neither slice.
	callTool(t, create, "u1", map[string]any{
		"type": "income", "amount": 5000.0, "category": "outros_receitas",
		"origem": "emprestimo", "date": "2026-07-07", "is_pending": false,
	})

	out := callTool(t, handlerFor(t, store, "get_resumo_mensal"), "u1",
		map[string]any{"month": "2026-07"}).(map[string]any)

	if out["faturamento"] != 1000.0 {
		t.Fatalf("faturamento = %v, want 1000 — the empréstimo is not a sale", out["faturamento"])
	}
	rows, ok := out["faturamento_por_categoria"].([]map[string]any)
	if !ok {
		t.Fatalf("faturamento_por_categoria = %T, want a list", out["faturamento_por_categoria"])
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want varejo and atacado and nothing else", rows)
	}
	if rows[0]["category"] != "venda_varejo" || rows[0]["total"] != 600.0 {
		t.Errorf("first = %+v, want venda_varejo at 600", rows[0])
	}
	// Named the way the user named it, not by its slug.
	if rows[0]["label"] != "Venda Varejo" {
		t.Errorf("label = %v, want the label from the user's catalog", rows[0]["label"])
	}
	if rows[1]["category"] != "venda_atacado" || rows[1]["label"] != "Venda Atacado" {
		t.Errorf("second = %+v, want the default venda_atacado with its own label", rows[1])
	}

	// The parts add up to the whole they were split from.
	var sum float64
	for _, r := range rows {
		sum += r["total"].(float64)
	}
	if sum != out["faturamento"] {
		t.Errorf("the split totals %v, want it to decompose faturamento (%v)", sum, out["faturamento"])
	}
}

// Nothing sold, nothing to split: an empty list beside a zero says nothing the
// zero did not, and invites the model to describe a month that has no sales.
func TestResumoMensalToolOmitsTheSplitWithoutSales(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	callTool(t, handlerFor(t, store, "create_financial_entry"), "u1", map[string]any{
		"type": "expense", "amount": 100.0, "category": "aluguel",
		"date": "2026-07-05", "is_pending": false,
	})

	out := callTool(t, handlerFor(t, store, "get_resumo_mensal"), "u1",
		map[string]any{"month": "2026-07"}).(map[string]any)
	if _, ok := out["faturamento_por_categoria"]; ok {
		t.Errorf("faturamento_por_categoria = %v, want it absent for a month with no sales",
			out["faturamento_por_categoria"])
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
		{UserID: "u1", EntryID: domain.EntryID("inc1"), TransactionDate: cd, Amount: 50000, Type: domain.EntryTypeIncome, Category: "venda_balcao", Origin: domain.OriginVenda, PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &cd, Source: domain.SourceManual},
		{UserID: "u1", EntryID: domain.EntryID("exp1"), TransactionDate: cd, Amount: 20000, Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &cd, Source: domain.SourceManual},
	} {
		if err := store.SaveEntry(ctx, e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Seed goal: R$ 1000 faturamento target, R$ 500 expense ceiling
	goal := domain.Goal{UserID: "u1", Month: month, RevenueTarget: 100000, ExpenseTarget: 50000}
	if err := store.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}

	h := handlerFor(t, store, "get_resumo_mensal")
	out := callTool(t, h, "u1", map[string]any{"month": month})

	m := out.(map[string]any)
	g := m["goal"].(map[string]any)

	if g["faturamento_target"] != 1000.0 {
		t.Fatalf("expected faturamento_target 1000, got %v", g["faturamento_target"])
	}
	if g["faturamento_progress_pct"] != 50.0 {
		t.Fatalf("expected faturamento_progress_pct 50, got %v", g["faturamento_progress_pct"])
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
		Amount: 1000, Type: domain.EntryTypeIncome, Origin: domain.OriginVenda,
		PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &cd, Source: domain.SourceManual,
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

func TestDefinirMetaPersisteFaturamentoTarget(t *testing.T) {
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
		t.Fatalf("expected IncomeTarget %d, got %d", reaisToCentavos(50000.0), goal.RevenueTarget)
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

	// Pre-save a goal with only faturamento target
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

func TestResumoMensalFaturamentoCappedAt100WhenExceedsTarget(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	month := "2026-07"

	cd := domain.NewCalendarDate(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	if err := store.SaveEntry(ctx, domain.FinancialEntry{
		UserID: "u1", EntryID: domain.EntryID("inc1"), TransactionDate: cd,
		Amount: 200000, Type: domain.EntryTypeIncome, Category: "venda_balcao", Origin: domain.OriginVenda,
		PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &cd, Source: domain.SourceManual,
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

	if g["faturamento_progress_pct"] != 100.0 {
		t.Fatalf("expected faturamento_progress_pct capped at 100, got %v", g["faturamento_progress_pct"])
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
	// Only expense target set, no faturamento target
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
	if _, ok := g["faturamento_progress_pct"]; ok {
		t.Fatal("expected no faturamento_progress_pct when faturamento target is 0")
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
		Amount: 50000, Type: domain.EntryTypeIncome, Origin: domain.OriginVenda,
		PaymentStatus: domain.PaymentStatusPaid, PaymentDate: &cd, Source: domain.SourceManual,
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

	results := entriesOf(t, out)
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
	all := entriesOf(t, outAll)
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

	results := entriesOf(t, out)
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

	results := entriesOf(t, out)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for category 'aluguel', got %d", len(results))
	}
}

func TestSearchEntriesByPeriod(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	// domain.AddMonths, not AddDate: on the 31st the latter overflows back into
	// the current month, which would put both entries inside the queried window.
	lastMonth := domain.AddMonths(now, -1)
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

	results := entriesOf(t, out)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for last month, got %d", len(results))
	}
	if results[0]["description"] != "Aluguel mês passado" {
		t.Fatalf("expected 'Aluguel mês passado', got %v", results[0]["description"])
	}
}

// seedPendingBills stores one pending bill per day of August 2026, alternating
// categories, so a listing over the whole month exceeds the default page.
func seedPendingBills(t *testing.T, store Store, userID string, days int) (total int64) {
	t.Helper()
	for day := 1; day <= days; day++ {
		due := domain.NewCalendarDate(time.Date(2026, 8, day, 0, 0, 0, 0, time.UTC))
		amount := int64(10_000 + day)
		category := "fornecedor_medicamentos"
		if day%3 == 0 {
			category = "emprestimo"
		}
		if err := store.SaveEntry(context.Background(), domain.FinancialEntry{
			UserID: userID, EntryID: domain.EntryID(fmt.Sprintf("b%02d", day)),
			TransactionDate: due, Amount: amount, Category: category,
			Type: domain.EntryTypeExpense, Description: fmt.Sprintf("Boleto %d", day),
			DueDate: &due, PaymentStatus: domain.PaymentStatusPending,
			Source: domain.SourceManual,
		}); err != nil {
			t.Fatalf("seed day %d: %v", day, err)
		}
		total += amount
	}
	return total
}

// The regression this whole envelope exists for: asked for a full month of
// contas a pagar, the tool used to return the most recent 20 and nothing else,
// so the model summed a third of August and reported it as the month's total.
//
// A named period is its own bound — the whole month comes back, and an
// explicit limit does not get to cut it.
func TestListDueEntriesReturnsThePeriodWholeIgnoringLimit(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	seedPendingBills(t, store, "u1", 60) // spills past August, so the window really filters

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{
		"from":  "2026-08-01",
		"to":    "2026-08-31",
		"limit": 10, // would have cut the month to 10 rows
	})

	env, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected listing envelope, got %T", out)
	}
	// August has 31 days, so exactly the first 31 seeded bills fall in the window.
	if got := len(entriesOf(t, out)); got != 31 {
		t.Fatalf("expected the whole month (31 rows), got %d — limit must not cut a period", got)
	}
	if env["truncated"] != false {
		t.Fatal("nothing was omitted, truncated must be false")
	}
	var inAugust int64
	for day := 1; day <= 31; day++ {
		inAugust += int64(10_000 + day)
	}
	if got := env["total_expense"]; got != centavosToReais(inAugust) {
		t.Fatalf("total_expense = %v, want %v — the total must cover the period",
			got, centavosToReais(inAugust))
	}
	if env["total_matching"] != 31 {
		t.Fatalf("total_matching = %v, want 31", env["total_matching"])
	}
}

// The backstop is not a page size: it only fires past maxRangeEntries, and
// when it does it announces itself instead of dropping rows quietly.
func TestListDueEntriesBackstopAnnouncesItself(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	var total int64
	// One day, many bills: comfortably past the backstop inside a single month.
	due := domain.NewCalendarDate(time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	for i := range maxRangeEntries + 7 {
		amount := int64(100 + i)
		total += amount
		if err := store.SaveEntry(ctx, domain.FinancialEntry{
			UserID: "u1", EntryID: domain.EntryID(fmt.Sprintf("b%04d", i)),
			TransactionDate: due, Amount: amount, Category: "fornecedor_medicamentos",
			Type: domain.EntryTypeExpense, Description: "Boleto",
			DueDate: &due, PaymentStatus: domain.PaymentStatusPending,
			Source: domain.SourceManual,
		}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{"from": "2026-08-01", "to": "2026-08-31"})
	env := out.(map[string]any)

	if got := len(entriesOf(t, out)); got != maxRangeEntries {
		t.Fatalf("expected %d rows at the backstop, got %d", maxRangeEntries, got)
	}
	if env["truncated"] != true {
		t.Fatal("the backstop must declare the cut, never swallow it")
	}
	if env["omitted"] != 7 {
		t.Fatalf("omitted = %v, want 7", env["omitted"])
	}
	if _, ok := env["warning"]; !ok {
		t.Fatal("a truncated result must carry a warning the model can relay")
	}
	// Even at the backstop the total still covers everything.
	if env["total_expense"] != centavosToReais(total) {
		t.Fatalf("total_expense = %v, want %v", env["total_expense"], centavosToReais(total))
	}
}

func TestListDueEntriesGroupsByCategory(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	seedPendingBills(t, store, "u1", 31)

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{"from": "2026-08-01", "to": "2026-08-31"})
	env := out.(map[string]any)

	cats, ok := env["by_category"].([]map[string]any)
	if !ok {
		t.Fatalf("expected by_category slice, got %T", env["by_category"])
	}
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d: %+v", len(cats), cats)
	}
	// Largest first, and labelled — the model should not have to translate slugs.
	if cats[0]["category"] != "fornecedor_medicamentos" {
		t.Fatalf("expected the largest category first, got %+v", cats[0])
	}
	if cats[0]["label"] != "Fornecedor de Medicamentos" {
		t.Fatalf("expected a human label, got %v", cats[0]["label"])
	}

	// The per-category totals must add back up to the period total.
	var sum float64
	for _, c := range cats {
		sum += c["total"].(float64)
	}
	if sum != env["total_expense"] {
		t.Fatalf("by_category sums to %v but total_expense is %v", sum, env["total_expense"])
	}
}

// A whole month has to come back with no limit argument at all — the default
// path is what the bot actually exercises.
func TestListDueEntriesFullMonthWithNoLimitArgument(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	seedPendingBills(t, store, "u1", 31)

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{"from": "2026-08-01", "to": "2026-08-31"})
	env := out.(map[string]any)

	if got := len(entriesOf(t, out)); got != 31 {
		t.Fatalf("expected all 31 bills in the detail list, got %d", got)
	}
	if env["truncated"] != false {
		t.Fatal("nothing was omitted, truncated must be false")
	}
	if _, ok := env["warning"]; ok {
		t.Fatal("no warning belongs on a complete result")
	}
}

// The first day of the range must survive: the old code ordered most-recent
// first and cut the tail, which is exactly how 01/08–20/08 disappeared.
func TestListDueEntriesKeepsTheStartOfThePeriod(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	seedPendingBills(t, store, "u1", 31)

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{"from": "2026-08-01", "to": "2026-08-31"})

	seen := make(map[string]bool)
	for _, r := range entriesOf(t, out) {
		seen[r["due_date"].(string)] = true
	}
	for _, day := range []string{"2026-08-01", "2026-08-15", "2026-08-31"} {
		if !seen[day] {
			t.Fatalf("entry due %s missing from the listing", day)
		}
	}
}

// Without a period there is nothing honest to sum over, so the tool must say
// so rather than hand back a total computed from one page.
func TestListingWithoutPeriodReportsNoTotals(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	seedPendingBills(t, store, "u1", 31)

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{"limit": 5})
	env := out.(map[string]any)

	if env["totals_available"] != false {
		t.Fatal("an unbounded listing must not claim totals")
	}
	if _, ok := env["total_expense"]; ok {
		t.Fatal("a partial total is worse than no total — it must be absent")
	}
	if env["truncated"] != true {
		t.Fatal("truncated must be true when more entries exist")
	}
}

// An absurd window (a hallucinated century) falls back to the paged path
// instead of reading the whole ledger to total it.
func TestListingRefusesToAggregateAnAbsurdSpan(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	seedPendingBills(t, store, "u1", 31)

	h := handlerFor(t, store, "list_due_entries")
	out := callTool(t, h, "u1", map[string]any{"from": "2000-01-01", "to": "2100-12-31"})
	env := out.(map[string]any)

	if env["totals_available"] != false {
		t.Fatal("a 100-year span must not be aggregated")
	}
}

func TestSearchEntriesCarriesTotalsForAPeriod(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	seedPendingBills(t, store, "u1", 31)

	h := handlerFor(t, store, "search_entries")
	out := callTool(t, h, "u1", map[string]any{
		"from":     "2026-08-01",
		"to":       "2026-08-31",
		"category": "emprestimo",
	})
	env := out.(map[string]any)

	if env["totals_available"] != true {
		t.Fatal("search over a period must carry totals too")
	}
	var want int64
	for day := 3; day <= 31; day += 3 {
		want += int64(10_000 + day)
	}
	if env["total_expense"] != centavosToReais(want) {
		t.Fatalf("total_expense = %v, want %v", env["total_expense"], centavosToReais(want))
	}
}
