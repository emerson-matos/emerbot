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
