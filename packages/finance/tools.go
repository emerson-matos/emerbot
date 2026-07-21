package finance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
)

// ToolHandler executes a single Gemini function call against the store. args is
// the raw JSON object the model produced for the call's parameters.
type ToolHandler func(ctx context.Context, userID string, args json.RawMessage) (any, error)

// financialCategories is the closed set of categories the model may assign when
// creating an entry — kept in sync with humanCategory in packages/whatsapp.
var financialCategories = []string{
	"aluguel", "folha_pagamento", "fornecedor_medicamentos", "fornecedor_geral",
	"impostos", "emprestimo", "cartao_credito", "energia_agua",
	"telefone_internet", "manutencao", "venda_balcao", "convenio", "delivery",
	"outros_despesas", "outros_receitas",
}

// knownCategory reports whether c is one of financialCategories. Tool args come
// from LLM output, so a hallucinated category is coerced to a default rather
// than persisted verbatim.
func knownCategory(c string) bool {
	for _, known := range financialCategories {
		if c == known {
			return true
		}
	}
	return false
}

// maxEntryAmountReais bounds a single entry's value. Tool args are LLM-generated
// from user text; a hallucinated absurd amount is rejected rather than saved.
const maxEntryAmountReais = 10_000_000

// FinanceTools returns the Gemini tool declarations exposed to the agent and a
// map from function name to the handler that runs it against store. The two are
// built together so a declaration can never drift from its handler.
func FinanceTools(store Store) ([]*genai.Tool, map[string]ToolHandler) {
	handlers := make(map[string]ToolHandler)

	tools := []*genai.Tool{
		createEntryTool(handlers, store),
		editEntryTool(handlers, store),
		monthSummaryTool(handlers, store),
		listDueEntriesTool(handlers, store),
		searchEntriesTool(handlers, store),
	}

	return tools, handlers
}

// --- create_financial_entry ---

func createEntryTool(handlers map[string]ToolHandler, store Store) *genai.Tool {
	const name = "create_financial_entry"

	decl := &genai.FunctionDeclaration{
		Name:        name,
		Description: "Cria um novo lançamento financeiro (despesa, receita, conta a pagar/receber).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"type":        {Type: genai.TypeString, Enum: []string{"expense", "income"}},
				"amount":      {Type: genai.TypeNumber, Description: "Valor em reais (ex: 500.00)"},
				"category":    {Type: genai.TypeString, Enum: financialCategories, Description: "Categoria do lançamento"},
				"description": {Type: genai.TypeString, Description: "Descrição curta do lançamento"},
				"date":        {Type: genai.TypeString, Description: "Data da transação YYYY-MM-DD (padrão: hoje)"},
				"due_date":    {Type: genai.TypeString, Description: "Data de vencimento YYYY-MM-DD (para contas a pagar/receber)"},
				"is_pending":  {Type: genai.TypeBoolean, Description: "true = a pagar/receber, false = já pago/recebido"},
			},
			Required: []string{"type", "amount", "category", "is_pending"},
		},
	}

	handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
		var args struct {
			Type        string  `json:"type"`
			Amount      float64 `json:"amount"`
			Category    string  `json:"category"`
			Description string  `json:"description"`
			Date        string  `json:"date"`
			DueDate     string  `json:"due_date"`
			IsPending   bool    `json:"is_pending"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}
		if args.Amount <= 0 || args.Amount > maxEntryAmountReais {
			return nil, fmt.Errorf("invalid amount: %v", args.Amount)
		}
		if args.Type != "expense" && args.Type != "income" {
			return nil, fmt.Errorf("invalid type: %q (expected expense or income)", args.Type)
		}

		now := time.Now().UTC()
		entry := domain.FinancialEntry{
			UserID:      userID,
			EntryID:     uuid.New().String(),
			Date:        now,
			Amount:      reaisToCentavos(args.Amount),
			Category:    args.Category,
			Description: args.Description,
			Source:      "whatsapp",
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		entry.Type = domain.EntryTypeExpense
		if args.Type == "income" {
			entry.Type = domain.EntryTypeIncome
		}

		// Coerce an empty or hallucinated category to the type's default rather
		// than persisting an out-of-set value.
		if !knownCategory(entry.Category) {
			entry.Category = "outros_despesas"
			if entry.Type == domain.EntryTypeIncome {
				entry.Category = "outros_receitas"
			}
		}

		if d, ok := parseDate(args.Date); ok {
			entry.Date = d
		}

		entry.PaymentStatus = domain.PaymentStatusPaid
		if args.IsPending {
			entry.PaymentStatus = domain.PaymentStatusPending
			// A due date only means something for a pending (a-pagar/receber)
			// entry; ignore it for one already settled.
			if d, ok := parseDate(args.DueDate); ok {
				entry.DueDate = &d
			}
		} else {
			entry.PaymentDate = &entry.Date
		}

		if err := store.SaveEntry(ctx, entry); err != nil {
			return nil, fmt.Errorf("save entry: %w", err)
		}

		return map[string]any{
			"entry_id": entry.EntryID,
			"status":   "created",
			"amount":   centavosToReais(entry.Amount),
			"category": entry.Category,
		}, nil
	}

	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- edit_financial_entry ---

func editEntryTool(handlers map[string]ToolHandler, store Store) *genai.Tool {
	const name = "edit_financial_entry"

	decl := &genai.FunctionDeclaration{
		Name: name,
		Description: "Edita um lançamento financeiro existente (encontrado via " +
			"search_entries ou list_due_entries). Só os campos informados são alterados.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"entry_id":    {Type: genai.TypeString, Description: "ID do lançamento a editar"},
				"amount":      {Type: genai.TypeNumber, Description: "Novo valor em reais (ex: 500.00)"},
				"category":    {Type: genai.TypeString, Enum: financialCategories, Description: "Nova categoria do lançamento"},
				"description": {Type: genai.TypeString, Description: "Nova descrição do lançamento"},
				"date":        {Type: genai.TypeString, Description: "Nova data da transação YYYY-MM-DD"},
				"due_date":    {Type: genai.TypeString, Description: "Nova data de vencimento YYYY-MM-DD"},
				"is_pending":  {Type: genai.TypeBoolean, Description: "true = a pagar/receber, false = já pago/recebido"},
			},
			Required: []string{"entry_id"},
		},
	}

	handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
		var args struct {
			EntryID     string  `json:"entry_id"`
			Amount      float64 `json:"amount"`
			Category    string  `json:"category"`
			Description string  `json:"description"`
			Date        string  `json:"date"`
			DueDate     string  `json:"due_date"`
			IsPending   *bool   `json:"is_pending"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}
		if args.EntryID == "" {
			return nil, fmt.Errorf("entry_id is required")
		}

		entry, err := store.GetEntry(ctx, userID, args.EntryID)
		if err != nil {
			return nil, fmt.Errorf("get entry: %w", err)
		}

		if args.Amount != 0 {
			if args.Amount <= 0 || args.Amount > maxEntryAmountReais {
				return nil, fmt.Errorf("invalid amount: %v", args.Amount)
			}
			entry.Amount = reaisToCentavos(args.Amount)
		}
		if args.Category != "" && knownCategory(args.Category) {
			entry.Category = args.Category
		}
		if args.Description != "" {
			entry.Description = args.Description
		}
		if d, ok := parseDate(args.Date); ok {
			entry.Date = d
		}
		if d, ok := parseDate(args.DueDate); ok {
			entry.DueDate = &d
		}
		if args.IsPending != nil {
			if *args.IsPending {
				entry.PaymentStatus = domain.PaymentStatusPending
				entry.PaymentDate = nil
			} else {
				entry.PaymentStatus = domain.PaymentStatusPaid
				if entry.PaymentDate == nil {
					now := time.Now().UTC()
					entry.PaymentDate = &now
				}
			}
		}

		entry.UpdatedAt = time.Now().UTC()

		if err := store.UpdateEntry(ctx, entry); err != nil {
			return nil, fmt.Errorf("update entry: %w", err)
		}

		return map[string]any{
			"entry_id": entry.EntryID,
			"status":   "updated",
			"amount":   centavosToReais(entry.Amount),
			"category": entry.Category,
		}, nil
	}

	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- get_month_summary ---

func monthSummaryTool(handlers map[string]ToolHandler, store Store) *genai.Tool {
	const name = "get_month_summary"

	decl := &genai.FunctionDeclaration{
		Name:        name,
		Description: "Retorna o resumo financeiro de um mês: receitas, despesas e saldo.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"month": {Type: genai.TypeString, Description: "Mês no formato YYYY-MM (padrão: mês atual)"},
			},
		},
	}

	handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
		var args struct {
			Month string `json:"month"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}
		if args.Month == "" {
			args.Month = time.Now().UTC().Format("2006-01")
		}

		summary, err := store.MonthlySummary(ctx, userID, args.Month)
		if err != nil {
			return nil, fmt.Errorf("monthly summary: %w", err)
		}

		return map[string]any{
			"month":   summary.Month,
			"income":  centavosToReais(summary.TotalIncome),
			"expense": centavosToReais(summary.TotalExpense),
			"balance": centavosToReais(summary.Balance),
		}, nil
	}

	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- list_due_entries ---

func listDueEntriesTool(handlers map[string]ToolHandler, store Store) *genai.Tool {
	const name = "list_due_entries"

	decl := &genai.FunctionDeclaration{
		Name:        name,
		Description: "Lista contas a pagar ou receber em um período de datas.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"from":   {Type: genai.TypeString, Description: "Data inicial YYYY-MM-DD"},
				"to":     {Type: genai.TypeString, Description: "Data final YYYY-MM-DD"},
				"status": {Type: genai.TypeString, Enum: []string{"pending", "paid"}},
				"limit":  {Type: genai.TypeInteger, Description: "Máximo de resultados (padrão: 20)"},
			},
		},
	}

	handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
		var args struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Status string `json:"status"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}

		filter := EntryFilter{Limit: clampLimit(args.Limit)}
		if d, ok := parseDate(args.From); ok {
			filter.From = &d
		}
		if d, ok := parseDate(args.To); ok {
			filter.To = &d
		}
		switch args.Status {
		case "pending":
			filter.Status = domain.PaymentStatusPending
		case "paid":
			filter.Status = domain.PaymentStatusPaid
		default:
			// A "due" question is about what's still owed unless told otherwise.
			filter.Status = domain.PaymentStatusPending
		}

		entries, err := store.ListEntries(ctx, userID, filter)
		if err != nil {
			return nil, fmt.Errorf("list entries: %w", err)
		}
		return entriesToMaps(entries), nil
	}

	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- search_entries ---

func searchEntriesTool(handlers map[string]ToolHandler, store Store) *genai.Tool {
	const name = "search_entries"

	decl := &genai.FunctionDeclaration{
		Name:        name,
		Description: "Busca lançamentos por descrição, categoria ou período.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"query":    {Type: genai.TypeString, Description: "Texto para buscar na descrição"},
				"category": {Type: genai.TypeString, Description: "Filtrar por categoria"},
				"from":     {Type: genai.TypeString, Description: "Data inicial YYYY-MM-DD"},
				"to":       {Type: genai.TypeString, Description: "Data final YYYY-MM-DD"},
				"limit":    {Type: genai.TypeInteger, Description: "Máximo de resultados (padrão: 20)"},
			},
		},
	}

	handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
		var args struct {
			Query    string `json:"query"`
			Category string `json:"category"`
			From     string `json:"from"`
			To       string `json:"to"`
			Limit    int    `json:"limit"`
		}
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, fmt.Errorf("parse args: %w", err)
		}

		filter := EntryFilter{
			Category:    args.Category,
			Description: strings.TrimSpace(args.Query),
			Limit:       clampLimit(args.Limit),
		}
		if d, ok := parseDate(args.From); ok {
			filter.From = &d
		}
		if d, ok := parseDate(args.To); ok {
			filter.To = &d
		}

		entries, err := store.ListEntries(ctx, userID, filter)
		if err != nil {
			return nil, fmt.Errorf("search entries: %w", err)
		}
		return entriesToMaps(entries), nil
	}

	return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- shared helpers ---

func entriesToMaps(entries []domain.FinancialEntry) []map[string]any {
	results := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		results = append(results, entryToMap(e))
	}
	return results
}

func entryToMap(e domain.FinancialEntry) map[string]any {
	m := map[string]any{
		"entry_id":    e.EntryID,
		"type":        string(e.Type),
		"amount":      centavosToReais(e.Amount),
		"category":    e.Category,
		"description": e.Description,
		"date":        e.Date.Format("2006-01-02"),
		"status":      string(e.PaymentStatus),
	}
	if e.DueDate != nil {
		m["due_date"] = e.DueDate.Format("2006-01-02")
	}
	return m
}

// parseDate parses a "YYYY-MM-DD" string; ok is false for empty or malformed
// input so callers fall back to their default.
func parseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func clampLimit(n int) int {
	if n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

// reaisToCentavos converts a reais amount to integer centavos, rounding to the
// nearest centavo to avoid float truncation (e.g. 19.99 → 1999, not 1998).
func reaisToCentavos(reais float64) int64 {
	if reais < 0 {
		return -int64(-reais*100 + 0.5)
	}
	return int64(reais*100 + 0.5)
}

func centavosToReais(centavos int64) float64 {
	return float64(centavos) / 100
}
