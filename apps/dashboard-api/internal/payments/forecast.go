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
func combineForecast(base []pkgfinance.CashFlowPoint, receivables []payments.ExpectedReceivable) []ForecastPoint {
	byDay := make(map[string]int64, len(receivables))
	for _, r := range receivables {
		byDay[r.ExpectedDate.String()] += r.Amount
	}

	var running int64
	if len(base) > 0 {
		running = base[0].RunningBalance - base[0].ProjectedIncome + base[0].ProjectedExpense
	}

	points := make([]ForecastPoint, 0, len(base))
	for _, b := range base {
		rc := byDay[b.Date]
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
