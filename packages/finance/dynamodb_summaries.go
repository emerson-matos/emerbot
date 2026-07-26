package finance

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func (s *DynamoDBStore) MonthlySummary(ctx context.Context, userID, yearMonth string) (MonthlySummary, error) {
	from, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return MonthlySummary{}, fmt.Errorf("invalid yearMonth %q: %w", yearMonth, err)
	}
	to := from.AddDate(0, 1, -1)
	entries, err := s.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
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

// MultiMonthlySummary aggregates several months in a single query over the
// span they cover, rather than one query per month. The analysis needs a
// trailing three-month window on every request, and three sequential queries
// against the same partition is three times the latency and read cost for the
// same rows.
//
// Months need not be contiguous or sorted; entries outside the requested
// months are read but discarded, which is still cheaper than separate queries
// for any realistic window.
func (s *DynamoDBStore) MultiMonthlySummary(ctx context.Context, userID string, yearMonths []string) (map[string]MonthlySummary, error) {
	summaries, from, to, err := emptySummaries(yearMonths)
	if err != nil || len(summaries) == 0 {
		return summaries, err
	}

	entries, err := s.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}
	accumulateSummaries(summaries, entries)
	return summaries, nil
}

func (s *DynamoDBStore) CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]CategorySummary, error) {
	entries, err := s.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}

	totals := make(map[string]*CategorySummary)
	for _, e := range entries {
		if _, ok := totals[e.Category]; !ok {
			totals[e.Category] = &CategorySummary{Category: e.Category, Type: e.Type}
		}
		totals[e.Category].Total += e.Amount
		totals[e.Category].Count++
	}

	result := make([]CategorySummary, 0, len(totals))
	for _, v := range totals {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Total > result[j].Total
	})
	return result, nil
}

// CashFlowForecast projects daily running balance across the given calendar
// month (day 1 through the last day), not a rolling window centered on
// today — the dashboard always shows the current month.
func (s *DynamoDBStore) CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]CashFlowPoint, error) {
	from, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return nil, fmt.Errorf("invalid yearMonth %q: %w", yearMonth, err)
	}
	to := from.AddDate(0, 1, -1) // last day of the month
	days := int(to.Sub(from).Hours()/24) + 1

	entries, err := s.ListEntries(ctx, userID, EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, err
	}

	// Starting balance: sum everything strictly before the 1st of the
	// month. EntryFilter.To is inclusive, so bound it at the day before
	// "from" — otherwise day 1's entries would be counted twice (once here,
	// once in byDay below).
	dayBeforeFrom := from.AddDate(0, 0, -1)
	startEntries, err := s.ListEntries(ctx, userID, EntryFilter{To: &dayBeforeFrom})
	if err != nil {
		return nil, err
	}
	var running int64
	for _, e := range startEntries {
		if e.Type == domain.EntryTypeIncome {
			running += e.Amount
		} else {
			running -= e.Amount
		}
	}

	type dayTotals struct{ income, expense int64 }
	byDay := make(map[string]*dayTotals)
	for _, e := range entries {
		day := EffectiveDate(e).Format("2006-01-02")
		if _, ok := byDay[day]; !ok {
			byDay[day] = &dayTotals{}
		}
		if e.Type == domain.EntryTypeIncome {
			byDay[day].income += e.Amount
		} else {
			byDay[day].expense += e.Amount
		}
	}

	points := make([]CashFlowPoint, 0, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		day := d.Format("2006-01-02")
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
