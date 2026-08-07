package analytics

import (
	"sort"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// expenseComposition breaks the month's expenses down by category, largest
// share first. An empty result means there were no expenses at all — the
// percentages would otherwise be a division by zero.
func expenseComposition(entries []domain.FinancialEntry, labels map[string]string) []CategoryComposition {
	return composition(entries, labels, func(e domain.FinancialEntry) bool {
		return e.Type == domain.EntryTypeExpense
	})
}

// revenueComposition breaks the month's faturamento down by category: not how
// much the pharmacy sold, but what kind of selling it was — atacado, balcão,
// convênio, delivery.
//
// What counts as a sale is still domain.IsRevenue, and still nothing to do with
// the category (ADR-016): an income entry filed under a brand-new category is a
// sale because its origin says "venda", and one filed under "Venda Balcão" with
// origin "emprestimo" is not one. The category only splits what is already
// faturamento, which is why this decomposes KPIs.Faturamento exactly.
func revenueComposition(entries []domain.FinancialEntry, labels map[string]string) []CategoryComposition {
	return composition(entries, labels, domain.IsRevenue)
}

// composition is the shared fold: one quantity, split by category, as amounts
// and as whole percentages of the total.
func composition(entries []domain.FinancialEntry, labels map[string]string, keep func(domain.FinancialEntry) bool) []CategoryComposition {
	byCategory := map[string]int64{}
	var total int64
	for _, e := range entries {
		if !keep(e) {
			continue
		}
		byCategory[e.Category] += e.Amount
		total += e.Amount
	}
	if total == 0 {
		return []CategoryComposition{}
	}

	out := make([]CategoryComposition, 0, len(byCategory))
	for id, amount := range byCategory {
		out = append(out, CategoryComposition{
			CategoryID:   id,
			CategoryName: categoryLabel(labels, id),
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

// categoryLabel turns a category slug into something readable, preferring the
// label its owner gave it (Input.CategoryLabels) over the default definitions,
// and title-casing the slug when neither knows it.
//
// The order matters now that the catalog is the user's to extend: "venda_
// varejo" is not in the defaults and never will be, so a rendering that only
// consulted them would print the slug at the very customer who named it.
func categoryLabel(labels map[string]string, slug string) string {
	return pkgfinance.CategoryLabel(labels, slug)
}
