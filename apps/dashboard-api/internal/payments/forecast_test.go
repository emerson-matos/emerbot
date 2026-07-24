package payments

import (
	"testing"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/payments"
)

func mustDate(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return d
}

// The ledger's cash-flow points already encode a pre-month starting balance in
// day 1's RunningBalance. combineForecast must recover that seed and add
// receivables as income on their expected day.
func TestCombineForecastAddsReceivables(t *testing.T) {
	// Ledger: starting balance 1000 (before the month), then day 1 has income
	// 200 (running 1200), day 2 has expense 500 (running 700).
	base := []pkgfinance.CashFlowPoint{
		{Date: "2026-07-01", ProjectedIncome: 200, ProjectedExpense: 0, RunningBalance: 1200},
		{Date: "2026-07-02", ProjectedIncome: 0, ProjectedExpense: 500, RunningBalance: 700},
	}
	recv := []payments.ExpectedReceivable{
		{Provider: payments.ProviderPagBank, SaleID: "pagbank:S1", ExpectedDate: mustDate(t, "2026-07-02"), Amount: 300, InstallmentNumber: 1, InstallmentTotal: 1},
	}

	points := combineForecast(base, recv)
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	// Day 1: seed 1000 + income 200 = 1200 (no receivable).
	if points[0].RunningBalance != 1200 || points[0].ProjectedReceivable != 0 {
		t.Errorf("day 1 = %+v, want running 1200, receivable 0", points[0])
	}
	// Day 2: 1200 + receivable 300 − expense 500 = 1000.
	if points[1].RunningBalance != 1000 || points[1].ProjectedReceivable != 300 {
		t.Errorf("day 2 = %+v, want running 1000, receivable 300", points[1])
	}
}

func TestCombineForecastEmptyBase(t *testing.T) {
	if got := combineForecast(nil, nil); len(got) != 0 {
		t.Errorf("empty base should yield no points, got %+v", got)
	}
}
