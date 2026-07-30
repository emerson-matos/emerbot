package finance

import (
	"context"
	"encoding/json"
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
		createEntryTool(store),
		editEntryTool(store, loc),
		resumoMensalTool(store),
		definirMetaTool(store),
		listDueEntriesTool(store),
		searchEntriesTool(store),
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

// --- create_financial_entry ---

func createEntryTool(store Store) Tool {
	const name = "create_financial_entry"

	return Tool{
		Name:        name,
		Description: "Cria um novo lançamento financeiro (despesa, receita, conta a pagar/receber).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"type":        {Type: genai.TypeString, Enum: []string{"expense", "income"}},
				"amount":      {Type: genai.TypeNumber, Description: "Valor em reais (ex: 500.00)"},
				"category":    {Type: genai.TypeString, Enum: categorySlugs(), Description: "Categoria do lançamento"},
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

			now := time.Now().UTC()
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

			entry.Type = domain.EntryTypeExpense
			if args.Type == "income" {
				entry.Type = domain.EntryTypeIncome
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

			if !knownCategory(entry.Category) {
				entry.Category = "outros_despesas"
				if entry.Type == domain.EntryTypeIncome {
					entry.Category = "outros_receitas"
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
				"category":    {Type: genai.TypeString, Enum: categorySlugs(), Description: "Nova categoria do lançamento"},
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

			if err := store.UpdateEntry(ctx, entry); err != nil {
				return nil, fmt.Errorf("update entry: %w", err)
			}

			return map[string]any{
				"entry_id": entry.EntryID,
				"status":   "updated",
				"amount":   centavosToReais(entry.Amount),
				"category": entry.Category,
			}, nil
		},
	}
}

// --- get_resumo_mensal ---

func resumoMensalTool(store Store) Tool {
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
				args.Month = domain.CurrentMonth()
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

func definirMetaTool(store Store) Tool {
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
				month = domain.CurrentMonth()
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
func listing(ctx context.Context, store EntryLister, userID string, filter EntryFilter, toolName string) (map[string]any, error) {
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
		byCategory := foldByCategory(matched)
		cats := make([]map[string]any, 0, len(byCategory))
		for _, c := range byCategory {
			cats = append(cats, map[string]any{
				"category": c.Category,
				"label":    c.Label,
				"type":     string(c.Type),
				"total":    centavosToReais(c.Total),
				"count":    c.Count,
			})
		}
		result["totals_available"] = true
		result["total_matching"] = len(matched)
		result["total_expense"] = centavosToReais(expense)
		result["total_entradas"] = centavosToReais(entradas)
		result["total_faturamento"] = centavosToReais(faturamento)
		result["by_category"] = cats
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
				len(shown), len(matched), omitted, len(matched))
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
					defaultEntryLimit, maxEntryLimit)},
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
					defaultEntryLimit, maxEntryLimit)},
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
