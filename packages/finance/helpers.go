package finance

import (
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

// RevenueIncome sums the Amount of entries that are faturamento — revenue
// earned by selling something, as opposed to money that merely moved.
//
// This used to sum only entries in the "venda_balcao" category, on the theory
// that convênio and delivery receipts would flatter the number the goal is
// tracked against. In practice that just made "Faturamento" here disagree
// with "Receita" everywhere else in the app for the same month, since every
// income category the pharmacy has (venda_balcao, convenio, delivery,
// outros_receitas) is a sale. If a non-operational income category is ever
// added — a loan disbursement, a partner contribution — it must be excluded
// here: cash coming in is not the same thing as faturamento.
func RevenueIncome(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if e.Type == domain.EntryTypeIncome {
			total += e.Amount
		}
	}
	return total
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
