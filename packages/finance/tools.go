package finance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
)

// ToolFunc executes a single Gemini function call against the store. args is
// the raw JSON object the model produced for the call's parameters.
type ToolFunc func(ctx context.Context, userID string, args json.RawMessage) (any, error)

// Tool bundles a Gemini function-call declaration with the handler that
// executes it against a Store.
type Tool struct {
	Name        string
	Description string
	Parameters  *genai.Schema
	Handler     ToolFunc
}

// FinanceTools builds the set of financial tools exposed to the Gemini agent.
// dashboardURL, when non-empty, includes a get_dashboard_link tool so the model
// can respond to "qual o link do dashboard?" with the real dashboard URL.
// FinanceTools builds the tool set. loc is the calendar "today" is resolved
// in when a tool settles an entry; nil falls back to UTC.
func FinanceTools(store Store, dashboardURL string, loc *time.Location) []Tool {
	if loc == nil {
		loc = time.UTC
	}
	tools := []Tool{
		createEntryTool(store, loc),
		editEntryTool(store, loc),
		resumoMensalTool(store, loc),
		definirMetaTool(store, loc),
		listDueEntriesTool(store),
		searchEntriesTool(store),
		listCategoriesTool(store),
		createCategoryTool(store),
	}
	if dashboardURL != "" {
		tools = append(tools, dashboardLinkTool(dashboardURL))
	}
	return tools
}

// --- get_dashboard_link ---

func dashboardLinkTool(url string) Tool {
	const name = "get_dashboard_link"

	return Tool{
		Name:        name,
		Description: "Retorna o link e a descrição do dashboard financeiro, onde o usuário pode ver gráficos, fluxo de caixa e gerenciar lançamentos.",
		Parameters: &genai.Schema{
			Type:       genai.TypeObject,
			Properties: map[string]*genai.Schema{},
		},
		Handler: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return map[string]any{
				"url":         url,
				"description": "Dashboard financeiro da Farmácia — gráficos de receitas e despesas, fluxo de caixa diário, metas e gerenciamento de lançamentos.",
			}, nil
		},
	}
}

// --- list_categories ---

const listCategoriesToolName = "list_categories"

// listCategoriesTool is how the model learns which categories exist. It has to
// exist for the same reason the enum had to go: the schema can only carry the
// defaults, and the ones this user added are exactly the ones a question about
// "minhas vendas no atacado" is about.
func listCategoriesTool(store CategoryReader) Tool {
	return Tool{
		Name: listCategoriesToolName,
		Description: "Lista as categorias disponíveis para lançamentos deste usuário — " +
			"as padrão do sistema e as que ele já criou. Use antes de criar uma " +
			"categoria nova (para não duplicar) e sempre que precisar do slug exato.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"type": {
					Type: genai.TypeString, Enum: []string{"expense", "income"},
					Description: "Filtra por tipo de lançamento (padrão: todas)",
				},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Type string `json:"type"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, fmt.Errorf("parse args: %w", err)
				}
			}

			cat, err := loadCatalog(ctx, store, userID)
			if err != nil {
				return nil, err
			}

			cats := make([]map[string]any, 0, len(cat.bySlug))
			for _, c := range cat.list() {
				if args.Type != "" && string(c.Type) != args.Type {
					continue
				}
				cats = append(cats, map[string]any{
					"slug":  c.Slug,
					"label": c.Label,
					"tipo":  string(c.Type),
				})
			}
			return map[string]any{
				"categorias": cats,
				"count":      len(cats),
			}, nil
		},
	}
}

// --- create_category ---

const createCategoryToolName = "create_category"

// createCategoryTool lets the pharmacy name its own kinds of movement from
// WhatsApp — "venda no atacado", "venda no varejo", a supplier it buys from
// every week — without opening the dashboard.
//
// What it deliberately does not do is create a *different kind* of category.
// The slug is derived by domain.NewCategory, the same function the dashboard
// form goes through, so a category born in a chat is indistinguishable from one
// born in a form and nothing downstream has to know which it was. And it does
// not touch faturamento: a new income category is still only revenue when the
// entry's origin is "venda" (ADR-016) — the category says which kind of sale it
// was, never whether it was one.
func createCategoryTool(store CategoryStore) Tool {
	return Tool{
		Name: createCategoryToolName,
		Description: "Cria uma categoria nova para este usuário (despesa ou receita), " +
			"no mesmo padrão das categorias do sistema. Use quando ele quiser separar um " +
			"tipo de venda ou de gasto que ainda não existe — por exemplo \"venda atacado\" " +
			"e \"venda varejo\". Chame " + listCategoriesToolName + " antes para não " +
			"duplicar uma que já existe. Criar categoria de entrada não muda o que é " +
			"faturamento: isso continua sendo a origem \"venda\".",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"label": {
					Type:        genai.TypeString,
					Description: "Nome da categoria como o usuário fala dela (ex: \"Venda Atacado\")",
				},
				"type": {
					Type: genai.TypeString, Enum: []string{"expense", "income"},
					Description: "expense = saída de dinheiro, income = entrada",
				},
			},
			Required: []string{"label", "type"},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Label string `json:"label"`
				Type  string `json:"type"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}

			cat, err := domain.NewCategory(userID, args.Label, "", domain.EntryType(args.Type))
			if err != nil {
				return nil, errors.New(
					"categoria inválida: informe um label com letras ou números e type \"expense\" ou \"income\"")
			}

			existing, err := loadCatalog(ctx, store, userID)
			if err != nil {
				return nil, err
			}
			// Already there: reported as such rather than written again. SaveCategory
			// is a PutItem, so re-creating "Venda Atacado" after the user renamed it
			// to "Atacado (CNPJ)" would silently rename it back.
			if current, ok := existing.bySlug[cat.Slug]; ok {
				return map[string]any{
					"status":   "ja_existe",
					"slug":     current.Slug,
					"label":    current.Label,
					"tipo":     string(current.Type),
					"mensagem": "Essa categoria já existe — use o slug acima no lançamento.",
				}, nil
			}
			if len(existing.bySlug) >= maxUserCategories {
				return nil, fmt.Errorf(
					"o usuário já tem %d categorias, o máximo. Use uma das que existem (%s)",
					len(existing.bySlug), listCategoriesToolName)
			}

			if err := store.SaveCategory(ctx, cat); err != nil {
				return nil, fmt.Errorf("save category: %w", err)
			}
			return map[string]any{
				"status": "created",
				"slug":   cat.Slug,
				"label":  cat.Label,
				"tipo":   string(cat.Type),
			}, nil
		},
	}
}

// --- create_financial_entry ---

func createEntryTool(store Store, loc *time.Location) Tool {
	const name = "create_financial_entry"

	return Tool{
		Name:        name,
		Description: "Cria um novo lançamento financeiro (despesa, receita, conta a pagar/receber).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"type":        {Type: genai.TypeString, Enum: []string{"expense", "income"}},
				"amount":      {Type: genai.TypeNumber, Description: "Valor em reais (ex: 500.00)"},
				"category":    {Type: genai.TypeString, Description: categoryArgDescription},
				"origem":      {Type: genai.TypeString, Enum: createOriginSlugs(), Description: originArgDescription},
				"description": {Type: genai.TypeString, Description: "Descrição curta do lançamento"},
				"date":        {Type: genai.TypeString, Description: "Data da transação YYYY-MM-DD (padrão: hoje)"},
				"due_date":    {Type: genai.TypeString, Description: "Data de vencimento YYYY-MM-DD (para contas a pagar/receber)"},
				"is_pending":  {Type: genai.TypeBoolean, Description: "true = a pagar/receber, false = já pago/recebido"},
			},
			Required: []string{"type", "amount", "category", "is_pending"},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Type        string  `json:"type"`
				Amount      float64 `json:"amount"`
				Category    string  `json:"category"`
				Origin      string  `json:"origem"`
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

			entryType := domain.EntryTypeExpense
			if args.Type == "income" {
				entryType = domain.EntryTypeIncome
			}
			// Before anything is written. The category used to be checked after the
			// entry was built and a bad one was quietly swapped for "Outros"; now
			// that the user's own categories are in play, a slug this catalog does
			// not have is a question for the model, not a bucket to dump the entry
			// in — see errUnknownCategory.
			cat, err := loadCatalog(ctx, store, userID)
			if err != nil {
				return nil, err
			}
			if err := cat.validate(args.Category, entryType); err != nil {
				return nil, err
			}

			now := time.Now().In(loc)
			entry, err := domain.NewFinancialEntry(domain.NewFinancialEntryInput{
				UserID:          userID,
				TransactionDate: domain.NewCalendarDate(now),
				Amount:          reaisToCentavos(args.Amount),
				Category:        args.Category,
				Description:     args.Description,
				Source:          domain.SourceWhatsApp,
			})
			if err != nil {
				return nil, err
			}

			entry.Type = entryType
			if entry.Type == domain.EntryTypeIncome {
				// An income entry always gets an origin. The model omitting it
				// most often means an ordinary sale, which is also the only
				// reading that keeps a pharmacy's day-to-day entries out of
				// "Outros" — but a hallucinated value falls back to
				// OriginOutros rather than quietly becoming revenue.
				entry.Origin = domain.OriginVenda
				if args.Origin != "" {
					entry.Origin = domain.NormalizeIncomeOrigin(args.Origin)
				}
			}

			if d, ok := parseDate(args.Date); ok {
				entry.TransactionDate = domain.NewCalendarDate(d)
			}

			entry.PaymentStatus = domain.PaymentStatusPaid
			if args.IsPending {
				entry.PaymentStatus = domain.PaymentStatusPending
				if d, ok := parseDate(args.DueDate); ok {
					date := domain.NewCalendarDate(d)
					entry.DueDate = &date
				}
			} else {
				date := entry.TransactionDate
				entry.PaymentDate = &date
			}

			if err := store.SaveEntry(ctx, entry); err != nil {
				return nil, fmt.Errorf("save entry: %w", err)
			}

			return map[string]any{
				"entry_id": entry.EntryID,
				"status":   "created",
				"amount":   centavosToReais(entry.Amount),
				"category": entry.Category,
				// The label as the user wrote it, so the confirmation says "Venda
				// Atacado" rather than reading the slug out loud.
				"category_label": CategoryLabel(cat.labels(), entry.Category),
			}, nil
		},
	}
}

// --- edit_financial_entry ---

func editEntryTool(store Store, loc *time.Location) Tool {
	const name = "edit_financial_entry"

	return Tool{
		Name: name,
		Description: "Edita um lançamento financeiro existente (encontrado via " +
			"search_entries ou list_due_entries). Só os campos informados são alterados.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"entry_id":    {Type: genai.TypeString, Description: "ID do lançamento a editar"},
				"amount":      {Type: genai.TypeNumber, Description: "Novo valor em reais (ex: 500.00)"},
				"category":    {Type: genai.TypeString, Description: "Nova categoria do lançamento. " + categoryArgDescription},
				"description": {Type: genai.TypeString, Description: "Nova descrição do lançamento"},
				"origem":      {Type: genai.TypeString, Enum: createOriginSlugs(), Description: originArgDescription},
				"date":        {Type: genai.TypeString, Description: "Nova data da transação YYYY-MM-DD"},
				"due_date":    {Type: genai.TypeString, Description: "Nova data de vencimento YYYY-MM-DD"},
				"is_pending":  {Type: genai.TypeBoolean, Description: "true = a pagar/receber, false = já pago/recebido"},
			},
			Required: []string{"entry_id"},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				EntryID     string  `json:"entry_id"`
				Amount      float64 `json:"amount"`
				Category    string  `json:"category"`
				Origin      string  `json:"origem"`
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

			// By ID alone: the model is handed entry IDs by search_entries and
			// has no dependable transaction date to pair them with, so this is
			// the one caller that pays for the partition read.
			entry, err := store.FindEntryByID(ctx, userID, args.EntryID)
			if err != nil {
				return nil, fmt.Errorf("get entry: %w", err)
			}
			// Kept before the edits below so the store knows which row to
			// replace — args.Date may move the entry to another sort key.
			previous := entry

			if args.Amount != 0 {
				if args.Amount <= 0 || args.Amount > maxEntryAmountReais {
					return nil, fmt.Errorf("invalid amount: %v", args.Amount)
				}
				entry.Amount = reaisToCentavos(args.Amount)
			}
			// Recategorizing is how "isso foi venda no atacado, não no balcão" gets
			// fixed, so an unknown slug is refused rather than dropped: the edit
			// used to ignore a category it did not recognise and report "updated",
			// which reads to the user as though the correction had been applied.
			labels := map[string]string{}
			if args.Category != "" {
				cat, err := loadCatalog(ctx, store, userID)
				if err != nil {
					return nil, err
				}
				if err := cat.validate(args.Category, entry.Type); err != nil {
					return nil, err
				}
				entry.Category = args.Category
				labels = cat.labels()
			}
			// Correcting the origin is how a mislabelled loan stops counting as
			// faturamento, so an edit has to be able to set it. Only on income:
			// domain.Normalize clears it on expenses anyway.
			if args.Origin != "" && entry.Type == domain.EntryTypeIncome {
				entry.Origin = domain.NormalizeIncomeOrigin(args.Origin)
			}
			if args.Description != "" {
				entry.Description = args.Description
			}
			if d, ok := parseDate(args.Date); ok {
				entry.TransactionDate = domain.NewCalendarDate(d)
			}
			if d, ok := parseDate(args.DueDate); ok {
				date := domain.NewCalendarDate(d)
				entry.DueDate = &date
			}
			if args.IsPending != nil {
				if *args.IsPending {
					entry.PaymentStatus = domain.PaymentStatusPending
					entry.PaymentDate = nil
				} else {
					entry.PaymentStatus = domain.PaymentStatusPaid
					if entry.PaymentDate == nil {
						// The pharmacy's calendar, not UTC: after 21:00 in
						// Brazil the UTC day is already tomorrow.
						date := domain.NewCalendarDate(time.Now().In(loc))
						entry.PaymentDate = &date
					}
				}
			}

			entry.UpdatedAt = time.Now().UTC()

			if err := store.UpdateEntry(ctx, previous, entry); err != nil {
				return nil, fmt.Errorf("update entry: %w", err)
			}

			return map[string]any{
				"entry_id":       entry.EntryID,
				"status":         "updated",
				"amount":         centavosToReais(entry.Amount),
				"category":       entry.Category,
				"category_label": CategoryLabel(labels, entry.Category),
			}, nil
		},
	}
}

// --- get_resumo_mensal ---

func resumoMensalTool(store Store, loc *time.Location) Tool {
	const name = "get_resumo_mensal"

	return Tool{
		Name:        name,
		Description: "Retorna o resumo financeiro de um mês: faturamento (só vendas), entradas de caixa (todo dinheiro que entrou, incluindo empréstimos e aportes), despesas, saldo e progresso das metas.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"month": {Type: genai.TypeString, Description: "Mês no formato YYYY-MM (padrão: mês atual)"},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Month string `json:"month"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if args.Month == "" {
				args.Month = domain.CurrentMonth(loc)
			}

			summary, err := store.MonthlySummary(ctx, userID, args.Month)
			if err != nil {
				return nil, fmt.Errorf("monthly summary: %w", err)
			}

			// faturamento and entradas_de_caixa are different questions and
			// deliberately different numbers: the first is what the pharmacy
			// sold, the second is every centavo that arrived, loans and aportes
			// included. Both come off the summary now — the goal progress below
			// used to re-read the whole month's entries because the summary
			// could not tell them apart.
			result := map[string]any{
				"month":             summary.Month,
				"faturamento":       centavosToReais(summary.TotalRevenue),
				"entradas_de_caixa": centavosToReais(summary.TotalCashIn),
				"expense":           centavosToReais(summary.TotalExpense),
				"balance":           centavosToReais(summary.ExpectedBalance),
				"goal":              nil,
			}

			// Faturamento broken into the kinds of sale behind it. A single figure
			// answers "quanto vendi" and nothing else, and a pharmacy that sells at
			// atacado and at balcão is asking which of the two moved — see ADR-024.
			// Only when there is faturamento to split: an empty list beside a zero
			// says nothing the zero did not.
			if summary.TotalRevenue > 0 {
				var labels map[string]string
				if cat, err := loadCatalog(ctx, store, userID); err == nil {
					labels = cat.labels()
				} else {
					log.Printf("finance tool %s: category labels for user %s: %v", name, userID, err)
				}
				byCategory, err := monthRevenueByCategory(ctx, store, userID, args.Month, labels)
				if err != nil {
					return nil, fmt.Errorf("faturamento por categoria: %w", err)
				}
				result["faturamento_por_categoria"] = categoryToolPayload(byCategory)
			}

			goal, err := store.GetGoal(ctx, userID, args.Month)
			if err == nil && (goal.RevenueTarget > 0 || goal.ExpenseTarget > 0) {
				faturamento := summary.TotalRevenue
				goalMap := map[string]any{
					"faturamento_target": centavosToReais(goal.RevenueTarget),
					"expense_target":     centavosToReais(goal.ExpenseTarget),
				}
				if goal.RevenueTarget > 0 {
					if faturamento <= goal.RevenueTarget {
						goalMap["faturamento_progress_pct"] = float64(faturamento*100) / float64(goal.RevenueTarget)
					} else {
						goalMap["faturamento_progress_pct"] = 100.0
					}
				}
				expense := summary.TotalExpense
				if goal.ExpenseTarget > 0 {
					if expense <= goal.ExpenseTarget {
						goalMap["expense_progress_pct"] = float64(expense*100) / float64(goal.ExpenseTarget)
					} else {
						goalMap["expense_progress_pct"] = 100.0
					}
				}
				result["goal"] = goalMap
			}

			return result, nil
		},
	}
}

// --- definir_meta ---

func definirMetaTool(store Store, loc *time.Location) Tool {
	const name = "definir_meta"

	return Tool{
		Name:        name,
		Description: "Define ou atualiza a meta mensal de faturamento e/ou teto de despesas. Pelo menos um dos valores deve ser informado.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"month":            {Type: genai.TypeString, Description: "Mês no formato YYYY-MM (padrão: mês atual)"},
				"meta_faturamento": {Type: genai.TypeNumber, Description: "Meta de faturamento em reais — mede apenas vendas (origem \"venda\"), nunca empréstimos, aportes ou outras entradas de caixa (ex: 80000.00)"},
				"teto_despesas":    {Type: genai.TypeNumber, Description: "Teto de despesas em reais (ex: 60000.00)"},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Month         string  `json:"month"`
				RevenueTarget float64 `json:"meta_faturamento"`
				ExpenseTarget float64 `json:"teto_despesas"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}

			month := args.Month
			if month == "" {
				month = domain.CurrentMonth(loc)
			}
			// The month comes from LLM output, so it is validated before it can
			// become a goal's key — an unchecked one stored "julho" verbatim and
			// no later read could ever match it.
			if _, _, err := domain.ParseMonth(month); err != nil {
				return nil, err
			}

			if args.RevenueTarget <= 0 && args.ExpenseTarget <= 0 {
				return nil, fmt.Errorf("informe pelo menos meta_faturamento ou teto_despesas")
			}

			goal := domain.Goal{
				UserID: userID,
				Month:  month,
			}

			if args.RevenueTarget > 0 {
				goal.RevenueTarget = reaisToCentavos(args.RevenueTarget)
			}
			if args.ExpenseTarget > 0 {
				goal.ExpenseTarget = reaisToCentavos(args.ExpenseTarget)
			}

			// Merge with existing goal if only one field was provided
			if args.RevenueTarget <= 0 || args.ExpenseTarget <= 0 {
				existing, err := store.GetGoal(ctx, userID, month)
				if err == nil {
					if args.RevenueTarget <= 0 {
						goal.RevenueTarget = existing.RevenueTarget
					}
					if args.ExpenseTarget <= 0 {
						goal.ExpenseTarget = existing.ExpenseTarget
					}
				}
			}

			if err := store.SaveGoal(ctx, goal); err != nil {
				return nil, fmt.Errorf("save goal: %w", err)
			}

			return map[string]any{
				"month":            month,
				"meta_faturamento": centavosToReais(goal.RevenueTarget),
				"teto_despesas":    centavosToReais(goal.ExpenseTarget),
				"status":           "saved",
			}, nil
		},
	}
}

// --- listing envelope, shared by list_due_entries and search_entries ---

// maxAggregateSpanDays bounds the window a listing tool treats as a period. A
// month, a quarter, even a year is fine — this exists only so a hallucinated
// "from 2000-01-01 to 2100-12-31" is not mistaken for a question about a
// period and turned into a full-partition read.
const maxAggregateSpanDays = 366

// maxRangeEntries is a backstop, not a page size: a period query returns the
// whole period, and this only stops a pathological ledger from being dumped
// into one prompt. At a pharmacy's volume a year of entries sits far below it,
// so in practice it never fires — and if it ever does, it fires loudly (see
// truncated/warning below) rather than quietly dropping rows.
const maxRangeEntries = 500

// listing runs filter and packages the outcome so a truncated page can never
// be mistaken for the whole period.
//
// The bug this exists to prevent: the tools used to return a bare array capped
// at 20 rows, ordered most-recent-first. Asked "quanto temos que pagar de
// 01/08 a 31/08", the model got the last 20 bills of August, no indication
// that anything was missing, and dutifully added them up — reporting a total
// that silently omitted the first third of the month. A wrong total that looks
// right is worse than an error, so:
//
//   - a query that names a period returns that period *whole*. The period is
//     already the bound; capping it again at some page size is what made a
//     question about August answerable with two thirds of August. limit does
//     not apply here — it is ignored, not clamped;
//   - the totals are computed over every matching entry, so the model never
//     has to add anything up itself — total_expense, total_entradas,
//     total_faturamento and by_category come pre-computed;
//   - if rows are ever omitted anyway (an unbounded query, or a period past
//     maxRangeEntries), truncated/omitted/warning say so out loud, and the
//     prompt requires the model to relay that.
//
// An unbounded query cannot be totalled honestly (there is no period to sum
// over without reading the whole ledger), so it returns rows only, and reports
// totals_available=false rather than a partial sum. That is the only path
// where limit still means anything.
// entryCatalogReader is what a listing needs: the entries, and the catalog that
// names their categories to the user.
type entryCatalogReader interface {
	EntryLister
	CategoryReader
}

func listing(ctx context.Context, store entryCatalogReader, userID string, filter EntryFilter, toolName string) (map[string]any, error) {
	bounded := filter.From != nil && filter.To != nil &&
		filter.To.Sub(*filter.From) <= maxAggregateSpanDays*24*time.Hour

	// A period is its own limit. Only an unbounded query pages.
	limit := maxRangeEntries
	query := filter
	if bounded {
		query.Limit = 0
	} else {
		limit = filter.Limit
		if limit <= 0 {
			limit = defaultEntryLimit
		}
		// One past the limit, purely to detect that more exist.
		query.Limit = limit + 1
	}

	matched, err := store.ListEntries(ctx, userID, query)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}

	shown := matched
	truncated := len(matched) > limit
	if truncated {
		shown = matched[:limit]
	}

	result := map[string]any{
		"entries":   entriesToMaps(shown),
		"count":     len(shown),
		"truncated": truncated,
	}
	if filter.From != nil && filter.To != nil {
		result["period"] = map[string]any{
			"from": filter.From.Format("2006-01-02"),
			"to":   filter.To.Format("2006-01-02"),
		}
	}

	if bounded {
		// total_entradas, not "total_income": these are the matched entries
		// summed over whatever period was asked for, on the effective-date
		// basis this listing filters by. That is neither faturamento (measured
		// on the day of the sale) nor entradas de caixa (measured on the day the
		// money arrived), so naming it after either would invite the model to
		// quote it as one. total_faturamento is offered alongside for the
		// question the model is usually being asked.
		var entradas, faturamento, expense int64
		for _, e := range matched {
			if e.Type != domain.EntryTypeIncome {
				expense += e.Amount
				continue
			}
			entradas += e.Amount
			if domain.IsRevenue(e) {
				faturamento += e.Amount
			}
		}
		// The user's own labels, so a category they created is named the way they
		// named it. A catalog that cannot be read is not worth failing a listing
		// over — CategoryLabel falls back to the defaults and then to the slug — so
		// the error is logged and the totals go out regardless.
		var labels map[string]string
		if cat, err := loadCatalog(ctx, store, userID); err == nil {
			labels = cat.labels()
		} else {
			log.Printf("finance tool %s: category labels for user %s: %v", toolName, userID, err)
		}

		result["totals_available"] = true
		result["total_matching"] = len(matched)
		result["total_expense"] = centavosToReais(expense)
		result["total_entradas"] = centavosToReais(entradas)
		result["total_faturamento"] = centavosToReais(faturamento)
		result["by_category"] = categoryToolPayload(foldByCategory(matched, labels, nil))
		// Which kinds of sale the faturamento above was made of. by_category
		// cannot answer that: it splits every entry, so an income category holds
		// sales and non-sales alike, and origin is what tells them apart (ADR-016).
		if faturamento > 0 {
			result["faturamento_por_categoria"] = categoryToolPayload(RevenueByCategory(matched, labels))
		}
	} else {
		result["totals_available"] = false
		result["note"] = "Sem período (from e to), não há como somar: informe as datas " +
			"para receber total_expense, total_entradas, total_faturamento e by_category já calculados."
	}

	if truncated {
		omitted := len(matched) - len(shown)
		result["omitted"] = omitted
		if bounded {
			result["warning"] = fmt.Sprintf(
				"Período grande demais para detalhar: %d de %d lançamentos aparecem em "+
					"'entries' (%d omitidos). Os totais acima cobrem TODOS os %d — use-os. "+
					"Avise o usuário que o detalhamento está parcial e ofereça consultar "+
					"um período menor.",
				len(shown), len(matched), omitted, len(matched),
			)
		} else {
			result["warning"] = "Lista incompleta e sem totais: informe from e to, ou aumente limit."
		}
		// Surfaces in CloudWatch, so a "the bot's total looks wrong" report can
		// be checked against the logs instead of guessed at.
		log.Printf("finance tool %s: truncated result for user %s: showing %d of %d entries (bounded=%t)",
			toolName, userID, len(shown), len(matched), bounded)
	}

	return result, nil
}

// --- list_due_entries ---

func listDueEntriesTool(store Store) Tool {
	const name = "list_due_entries"

	return Tool{
		Name: name,
		Description: "Lista contas a pagar ou receber em um período de datas. " +
			"Informando from e to, retorna o período INTEIRO (sem corte) mais os " +
			"totais já somados (total_expense, total_entradas, total_faturamento) e o " +
			"agrupamento por categoria (by_category) — use esses números, não some os lançamentos à mão.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"from":   {Type: genai.TypeString, Description: "Data inicial YYYY-MM-DD"},
				"to":     {Type: genai.TypeString, Description: "Data final YYYY-MM-DD"},
				"status": {Type: genai.TypeString, Enum: []string{"pending", "paid"}},
				"limit": {Type: genai.TypeInteger, Description: fmt.Sprintf(
					"Só se aplica a consultas SEM período (padrão: %d, máximo: %d). "+
						"Informando from e to, o período inteiro é retornado e este "+
						"limite é ignorado.",
					defaultEntryLimit, maxEntryLimit,
				)},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
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
				filter.Status = domain.PaymentStatusPending
			}

			return listing(ctx, store, userID, filter, name)
		},
	}
}

// --- search_entries ---

func searchEntriesTool(store Store) Tool {
	const name = "search_entries"

	return Tool{
		Name: name,
		Description: "Busca lançamentos por descrição, categoria ou período. " +
			"Informando from e to, retorna o período INTEIRO (sem corte) mais os " +
			"totais já somados (total_expense, total_entradas, total_faturamento) e o " +
			"agrupamento por categoria (by_category) — use esses números, não some os lançamentos à mão.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"query":    {Type: genai.TypeString, Description: "Texto para buscar na descrição"},
				"category": {Type: genai.TypeString, Description: "Filtrar por categoria"},
				"from":     {Type: genai.TypeString, Description: "Data inicial YYYY-MM-DD"},
				"to":       {Type: genai.TypeString, Description: "Data final YYYY-MM-DD"},
				"limit": {Type: genai.TypeInteger, Description: fmt.Sprintf(
					"Só se aplica a consultas SEM período (padrão: %d, máximo: %d). "+
						"Informando from e to, o período inteiro é retornado e este "+
						"limite é ignorado.",
					defaultEntryLimit, maxEntryLimit,
				)},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
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

			return listing(ctx, store, userID, filter, name)
		},
	}
}
