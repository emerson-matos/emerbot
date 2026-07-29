package finance

import (
	"context"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// The summaries are derived views over entries, not storage concerns:
// every one of them is "list the entries in a period, then fold them". They
// live here, once, over the EntryLister below, so both Store implementations
// share a single definition.
//
// They used to be written twice — once per Store — and the copies had drifted:
// an unparseable month errored on DynamoDB but returned a zero-valued summary
// on the in-memory store, so a typo'd month showed R$ 0,00 as though it were
// real data locally and a 500 in production. Deriving both from one function
// makes that class of divergence unrepresentable.

// EntryLister is the only capability the summaries need. Declaring it
// separately keeps them independent of the 18-method Store.
type EntryLister interface {
	ListEntries(ctx context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error)
}

// monthlySummary totals income and expense over one calendar month, bucketed
// by each entry's effectiveDate.
func monthlySummary(ctx context.Context, l EntryLister, userID, yearMonth string) (MonthlySummary, error) {
	from, to, err := domain.ParseMonth(yearMonth)
	if err != nil {
		return MonthlySummary{}, err
	}
	entries, err := l.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return MonthlySummary{}, err
	}

	summary := MonthlySummary{Month: yearMonth}
	for _, e := range entries {
		if e.Type == domain.EntryTypeIncome {
			summary.TotalIncome += e.Amount
		} else {
			summary.TotalExpense += e.Amount
		}
	}
	summary.Balance = summary.TotalIncome - summary.TotalExpense
	return summary, nil
}

// multiMonthlySummary aggregates several months in one query over the span they
// cover, rather than one query per month: the analysis needs a trailing
// three-month window on every request, and three sequential queries against the
// same partition is three times the latency and read cost for the same rows.
//
// Months need not be contiguous or sorted; entries outside the requested months
// are read but discarded, which is still cheaper than separate queries for any
// realistic window.
func multiMonthlySummary(ctx context.Context, l EntryLister, userID string, yearMonths []string) (map[string]MonthlySummary, error) {
	summaries, from, to, err := emptySummaries(yearMonths)
	if err != nil || len(summaries) == 0 {
		return summaries, err
	}

	entries, err := l.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}
	accumulateSummaries(summaries, entries)
	return summaries, nil
}

// categorySummary totals entries per category over an inclusive date range,
// largest total first.
func categorySummary(ctx context.Context, l EntryLister, userID string, from, to time.Time) ([]CategorySummary, error) {
	entries, err := l.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}
	return foldByCategory(entries), nil
}

// cashFlowForecast projects a daily running balance across the given calendar
// month (day 1 through the last day), not a rolling window centered on
// today — the dashboard always shows the current month.
func cashFlowForecast(ctx context.Context, l EntryLister, userID, yearMonth string) ([]CashFlowPoint, error) {
	from, to, err := domain.ParseMonth(yearMonth)
	if err != nil {
		return nil, err
	}
	days := int(to.Sub(from).Hours()/24) + 1

	entries, err := l.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}

	// Starting balance: everything strictly before the 1st. EntryFilter.To is
	// inclusive, so bound it at the day before "from" — otherwise day 1's
	// entries would be counted twice, once here and once in byDay below.
	dayBeforeFrom := from.AddDate(0, 0, -1)
	prior, err := l.ListEntries(ctx, userID, EntryFilter{To: &dayBeforeFrom})
	if err != nil {
		return nil, err
	}
	var running int64
	for _, e := range prior {
		if e.Type == domain.EntryTypeIncome {
			running += e.Amount
		} else {
			running -= e.Amount
		}
	}

	type dayTotals struct{ income, expense int64 }
	byDay := make(map[string]*dayTotals, days)
	for _, e := range entries {
		day := EffectiveDate(e).Format("2006-01-02")
		if byDay[day] == nil {
			byDay[day] = &dayTotals{}
		}
		if e.Type == domain.EntryTypeIncome {
			byDay[day].income += e.Amount
		} else {
			byDay[day].expense += e.Amount
		}
	}

	points := make([]CashFlowPoint, 0, days)
	for i := range days {
		day := from.AddDate(0, 0, i).Format("2006-01-02")
		var inc, exp int64
		if t := byDay[day]; t != nil {
			inc, exp = t.income, t.expense
		}
		running += inc - exp
		points = append(points, CashFlowPoint{
			Date:             day,
			ProjectedIncome:  inc,
			ProjectedExpense: exp,
			RunningBalance:   running,
		})
	}
	return points, nil
}
