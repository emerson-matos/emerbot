package payments

import (
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/payments"
)

// ForecastPoint is one day of the combined forecast: the ledger's projected
// income/expense (as the existing cash-flow view derives them) plus imported
// expected receivables, accumulated into a running balance seeded by the
// pharmacy's pre-month ledger balance.
type ForecastPoint struct {
	Date                string
	ProjectedIncome     int64 // from the FinancialEntry ledger
	ProjectedReceivable int64 // from imported PagBank receivables
	ProjectedExpense    int64
	RunningBalance      int64
}

// combineForecast folds imported receivables into the ledger's cash-flow points.
// It reuses base for the ledger amounts and the pre-month starting balance
// (base[0].RunningBalance minus that day's own net), then re-accumulates the
// running balance with receivables added as income on their expected day. This
// is the product's core value: the pharmacy's own balance + receivables −
// expenses — deliberately NOT PagBank's wallet balance.
//
// Receivables are added on top of the ledger's own ProjectedIncome, which
// assumes card sales reach the forecast through the import and NOT also as
// FinancialEntry income. That invariant is what keeps the projection honest: a
// card sale recorded in both places is counted twice here, and nothing
// downstream can detect it. See docs/adr/ADR-013-payment-imports.md.
//
// A receivable whose expected day is absent from base is carried into the first
// day of the projection rather than dropped. base spans the whole calendar month
// today, so this is defensive — but the failure it guards against is money
// silently vanishing from a cash-flow projection, which no error surfaces.
func combineForecast(base []pkgfinance.CashFlowPoint, receivables []payments.ExpectedReceivable) []ForecastPoint {
	byDay := make(map[string]int64, len(receivables))
	for _, r := range receivables {
		byDay[r.ExpectedDate.String()] += r.Amount
	}

	if len(base) == 0 {
		return nil
	}

	// Fold any receivable that has no matching ledger day into day one, so the
	// month's totals still account for every centavo imported.
	inBase := make(map[string]bool, len(base))
	for _, b := range base {
		inBase[b.Date] = true
	}
	var orphaned int64
	for day, amount := range byDay {
		if !inBase[day] {
			orphaned += amount
		}
	}

	running := base[0].RunningBalance - base[0].ProjectedIncome + base[0].ProjectedExpense

	points := make([]ForecastPoint, 0, len(base))
	for i, b := range base {
		rc := byDay[b.Date]
		if i == 0 {
			rc += orphaned
		}
		running += b.ProjectedIncome + rc - b.ProjectedExpense
		points = append(points, ForecastPoint{
			Date:                b.Date,
			ProjectedIncome:     b.ProjectedIncome,
			ProjectedReceivable: rc,
			ProjectedExpense:    b.ProjectedExpense,
			RunningBalance:      running,
		})
	}
	return points
}
