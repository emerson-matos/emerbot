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

// originArgDescription tells the model what the origin means and, crucially,
// that only "venda" is faturamento. Without the second half it happily files a
// loan as a sale, which is the whole mistake this field exists to prevent.
const originArgDescription = "Origem do dinheiro que entrou (só para type=income). " +
	"Use \"venda\" para qualquer venda de produto ou serviço — balcão, cartão, PIX ou crediário. " +
	"Use as outras para dinheiro que entrou sem venda: \"emprestimo\", \"aporte_socio\", " +
	"\"receita_financeira\" (rendimentos), \"restituicao\", \"outros\". " +
	"Apenas \"venda\" conta como faturamento e conta para a meta; as demais são só entrada de caixa. " +
	"Padrão: \"venda\"."

// createOriginSlugs is the origin enum offered to create_financial_entry.
//
// It deliberately omits OriginRecebimentoCliente. A customer settling a
// crediário sale is the *existing* pending entry being marked paid — the sale
// was already recorded on the day it happened. Offering the model an origin
// that sounds like "customer paid me" makes it create a second, paid entry for
// the same sale, and the money gets counted twice with nothing downstream able
// to detect it. That path is edit_financial_entry; see the rule in
// packages/orchestrator/internal/agentprompt. The origin stays reachable from
// the dashboard form, where a human can mean "a receivable that predates this
// ledger".
func createOriginSlugs() []string {
	slugs := make([]string, 0, len(domain.IncomeOrigins()))
	for _, o := range domain.IncomeOrigins() {
		if o == domain.OriginRecebimentoCliente {
			continue
		}
		slugs = append(slugs, string(o))
	}
	return slugs
}

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

// accumulateSummaries folds one basis's entries into the summaries they belong
// to. The month an entry belongs to depends on the basis — a sale is keyed by
// the month it was made, cash by the month it landed, a bill by the month it
// falls due — so the same entry can land in two different months across two
// calls, which is the whole point. Entries outside the requested months are
// ignored.
func accumulateSummaries(summaries map[string]MonthlySummary, basis DateBasis, entries []domain.FinancialEntry) {
	for _, e := range entries {
		date, ok := basisDate(basis, e)
		if !ok {
			continue
		}
		key := domain.MonthOf(date)
		summary, ok := summaries[key]
		if !ok {
			continue
		}
		addToSummary(&summary, basis, []domain.FinancialEntry{e})
		summaries[key] = summary
	}
}

// RevenueTotal sums faturamento across the given entries: what was sold, by
// domain.IsRevenue. This is the figure every performance indicator uses.
//
// It does not filter by date — callers pass the entries of the period they
// mean, and the period has to have been read on the transaction basis (see
// DateBasis) for the total to be the month's real faturamento.
func RevenueTotal(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if domain.IsRevenue(e) {
			total += e.Amount
		}
	}
	return total
}

// CashInTotal sums entradas de caixa: every inflow that has actually been
// received, whatever its origin. Pending entries are excluded — money that has
// not arrived is not cash.
func CashInTotal(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if e.Type == domain.EntryTypeIncome && CashDate(e) != nil {
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
