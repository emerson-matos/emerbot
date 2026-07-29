package finance

import (
	"sort"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// categorySlugs returns the set of known category slugs, derived from the
// domain definitions so the two never drift.
func categorySlugs() []string {
	cats := domain.DefaultCategories("")
	slugs := make([]string, len(cats))
	for i, c := range cats {
		slugs[i] = c.Slug
	}
	return slugs
}

// knownCategory reports whether c is one of categorySlugs. Tool args come
// from LLM output, so a hallucinated category is coerced to a default rather
// than persisted verbatim.
func knownCategory(c string) bool {
	for _, known := range categorySlugs() {
		if c == known {
			return true
		}
	}
	return false
}

// maxEntryAmountReais bounds a single entry's value. Tool args are LLM-generated
// from user text; a hallucinated absurd amount is rejected rather than saved.
const maxEntryAmountReais = 10_000_000

// parseDate parses a "YYYY-MM-DD" string; ok is false for empty or malformed
// input so callers fall back to their default. Tool args come from LLM output,
// where "no date given" and "a date I could not read" both mean "use the
// default" — which is why this reports a bool rather than an error.
func parseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := domain.ParseDay(s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// emptySummaries seeds one zero-valued summary per requested month and returns
// the calendar span they cover, so a multi-month aggregation can be served by
// a single date-range query. Duplicate months collapse into one entry.
//
// Requesting no months is not an error — it yields an empty map and a zero
// span, which callers short-circuit on rather than querying for nothing.
func emptySummaries(yearMonths []string) (map[string]MonthlySummary, time.Time, time.Time, error) {
	summaries := make(map[string]MonthlySummary, len(yearMonths))
	var from, to time.Time

	for _, ym := range yearMonths {
		start, end, err := domain.ParseMonth(ym)
		if err != nil {
			return nil, time.Time{}, time.Time{}, err
		}
		summaries[ym] = MonthlySummary{Month: ym}

		if from.IsZero() || start.Before(from) {
			from = start
		}
		if to.IsZero() || end.After(to) {
			to = end
		}
	}
	return summaries, from, to, nil
}

// accumulateSummaries folds entries into the summaries they belong to, keyed
// by the month of their effective date. Entries outside the requested months
// are ignored.
func accumulateSummaries(summaries map[string]MonthlySummary, entries []domain.FinancialEntry) {
	for _, e := range entries {
		key := domain.MonthOf(EffectiveDate(e))
		summary, ok := summaries[key]
		if !ok {
			continue
		}
		if e.Type == domain.EntryTypeIncome {
			summary.TotalIncome += e.Amount
		} else {
			summary.TotalExpense += e.Amount
		}
		summary.Balance = summary.TotalIncome - summary.TotalExpense
		summaries[key] = summary
	}
}

// IncomeTotal sums the Amount of every income entry — receita in the broad
// sense, whatever it came from. See FaturamentoTotal for the narrower figure
// goals are tracked against.
func IncomeTotal(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if e.Type == domain.EntryTypeIncome {
			total += e.Amount
		}
	}
	return total
}

// outrosReceitasCategory is the one income category that is not unambiguously
// a sale — the catch-all a person reaches for when nothing else fits, and so
// the one place a loan disbursement, a partner contribution or an investment
// redemption is likely to get filed for lack of a better bucket.
const outrosReceitasCategory = "outros_receitas"

// IsFaturamento reports whether an entry counts toward faturamento — revenue
// earned by selling something, as opposed to money that merely moved (an
// expense payment) or cash that came in without a sale behind it. It mirrors
// packages/finance/analytics.isFaturamento; that package cannot import this
// one's caller-facing helpers back without a cycle, so the two are kept in
// sync by hand.
//
// venda_balcao, convenio and delivery are unambiguous sales. outros_receitas
// is excluded on purpose — a goal is "quanto vendemos", and a loan or capital
// contribution logged under "Outros (Receita)" must not count toward it. That
// is exactly the "empréstimo não é faturamento" mistake this function exists
// to prevent.
func IsFaturamento(e domain.FinancialEntry) bool {
	return e.Type == domain.EntryTypeIncome && e.Category != outrosReceitasCategory
}

// FaturamentoTotal sums the Amount of entries that are IsFaturamento.
func FaturamentoTotal(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if IsFaturamento(e) {
			total += e.Amount
		}
	}
	return total
}

// defaultEntryLimit / maxEntryLimit bound how many rows a listing tool hands
// back to the model. The default used to be 20, which silently cut a normal
// month of contas a pagar in half — see listing() for why the cut is now
// visible, and why these are sized to fit a whole month of a small pharmacy's
// bills rather than a screenful.
const (
	defaultEntryLimit = 50
	maxEntryLimit     = 200
)

func clampLimit(n int) int {
	if n <= 0 {
		return defaultEntryLimit
	}
	if n > maxEntryLimit {
		return maxEntryLimit
	}
	return n
}

// categoryLabels maps category slug → human label, so a tool result can name
// "fornecedor_medicamentos" as "Fornecedor de Medicamentos" instead of leaving
// the model to invent a translation.
func categoryLabels() map[string]string {
	cats := domain.DefaultCategories("")
	labels := make(map[string]string, len(cats))
	for _, c := range cats {
		labels[c.Slug] = c.Label
	}
	return labels
}

// foldByCategory totals entries per category, largest total first. It is the
// one definition of "agrupado por categoria", shared by CategorySummary and by
// the listing tools' by_category block.
func foldByCategory(entries []domain.FinancialEntry) []CategorySummary {
	labels := categoryLabels()
	totals := make(map[string]*CategorySummary)
	for _, e := range entries {
		if _, ok := totals[e.Category]; !ok {
			totals[e.Category] = &CategorySummary{
				Category: e.Category,
				Label:    labels[e.Category],
				Type:     e.Type,
			}
		}
		totals[e.Category].Total += e.Amount
		totals[e.Category].Count++
	}

	result := make([]CategorySummary, 0, len(totals))
	for _, v := range totals {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Total != result[j].Total {
			return result[i].Total > result[j].Total
		}
		return result[i].Category < result[j].Category
	})
	return result
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
		"date":        e.TransactionDate.String(),
		"status":      string(e.PaymentStatus),
	}
	if e.DueDate != nil {
		m["due_date"] = e.DueDate.Format("2006-01-02")
	}
	return m
}
