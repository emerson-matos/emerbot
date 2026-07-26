package analytics

import (
	"sort"
	"strings"
	"unicode"

	"github.com/emerson/emerbot/packages/domain"
)

// expenseComposition breaks the month's expenses down by category, largest
// share first. An empty result means there were no expenses at all — the
// percentages would otherwise be a division by zero.
func expenseComposition(entries []domain.FinancialEntry) []ExpenseComposition {
	byCategory := map[string]int64{}
	var total int64
	for _, e := range entries {
		if e.Type != domain.EntryTypeExpense {
			continue
		}
		byCategory[e.Category] += e.Amount
		total += e.Amount
	}
	if total == 0 {
		return []ExpenseComposition{}
	}

	out := make([]ExpenseComposition, 0, len(byCategory))
	for id, amount := range byCategory {
		out = append(out, ExpenseComposition{
			CategoryID:   id,
			CategoryName: categoryLabel(id),
			Amount:       amount,
			Percentage:   roundToInt(float64(amount) / float64(total) * 100),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Amount != out[j].Amount {
			return out[i].Amount > out[j].Amount
		}
		return out[i].CategoryID < out[j].CategoryID
	})
	return out
}

// categoryLabel turns a category slug into something readable. Known slugs get
// their real label from the domain definitions; anything else (a legacy or
// hand-entered slug) falls back to title-casing the slug, which is what the
// dashboard did for every category before this moved to the backend.
func categoryLabel(slug string) string {
	for _, c := range domain.DefaultCategories("") {
		if c.Slug == slug {
			return c.Label
		}
	}
	words := strings.Split(strings.ReplaceAll(slug, "_", " "), " ")
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
