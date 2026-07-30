package analytics

import (
	"sort"

	"github.com/emerson/emerbot/packages/domain"
)

// maxCashOutDays caps how many heavy-spending days the analysis reports.
const maxCashOutDays = 5

type dayTotals struct {
	date string
	// revenue is faturamento only (see domain.IsRevenue) — what BestIncome and
	// WorstIncome are measured against, so a loan or aporte can't manufacture
	// a "best sales day".
	revenue int64
	// cashIn is every inflow, sale or not — what balance is measured against,
	// since a real cash movement must not be dropped just because it wasn't a
	// sale.
	cashIn  int64
	expense int64
	balance int64
}

// aggregateByDay folds entries into per-day totals, ordered oldest-first.
//
// It takes two slices because the two figures are measured on two different
// bases: revenueEntries on the transaction basis (a sale belongs to the day it
// was made) and entries on the effective-date basis (everything else). Passing
// the same slice twice would silently put an unpaid crediário sale on the wrong
// day.
func aggregateByDay(entries, revenueEntries []domain.FinancialEntry) []dayTotals {
	byDate := map[string]*dayTotals{}
	day := func(date string) *dayTotals {
		d, ok := byDate[date]
		if !ok {
			d = &dayTotals{date: date}
			byDate[date] = d
		}
		return d
	}
	for _, e := range revenueEntries {
		if domain.IsRevenue(e) {
			day(e.TransactionDate.String()).revenue += e.Amount
		}
	}
	for _, e := range entries {
		d := day(e.TransactionDate.String())
		if e.Type == domain.EntryTypeIncome {
			d.cashIn += e.Amount
		} else {
			d.expense += e.Amount
		}
		d.balance = d.cashIn - d.expense
	}

	days := make([]dayTotals, 0, len(byDate))
	for _, d := range byDate {
		days = append(days, *d)
	}
	// Sorted so ties resolve to the earliest day rather than to whatever order
	// the map happened to hand back.
	sort.Slice(days, func(i, j int) bool { return days[i].date < days[j].date })
	return days
}

// buildHighlights picks the best and worst day of the month by income and by
// balance. With no entries at all every slot is the same "Sem dados"
// placeholder, so the dashboard renders empty cards instead of a zero-value
// date.
func buildHighlights(entries, revenueEntries []domain.FinancialEntry) Highlights {
	days := aggregateByDay(entries, revenueEntries)
	if len(days) == 0 {
		empty := DayHighlight{Date: NoDataDate, Label: "Sem dados", Amount: 0}
		return Highlights{BestIncome: empty, WorstIncome: empty, BestBalance: empty, WorstBalance: empty}
	}

	bestIncome, worstIncome := days[0], days[0]
	bestBalance, worstBalance := days[0], days[0]
	for _, d := range days[1:] {
		if d.revenue > bestIncome.revenue {
			bestIncome = d
		}
		if d.revenue < worstIncome.revenue {
			worstIncome = d
		}
		if d.balance > bestBalance.balance {
			bestBalance = d
		}
		if d.balance < worstBalance.balance {
			worstBalance = d
		}
	}

	return Highlights{
		BestIncome:   toHighlight(bestIncome, bestIncome.revenue),
		WorstIncome:  toHighlight(worstIncome, worstIncome.revenue),
		BestBalance:  toHighlight(bestBalance, bestBalance.balance),
		WorstBalance: toHighlight(worstBalance, worstBalance.balance),
	}
}

func toHighlight(day dayTotals, amount int64) DayHighlight {
	label := day.date
	if d, err := domain.ParseCalendarDate(day.date); err == nil {
		label = dayMonthLabel(d.Time())
	}
	return DayHighlight{Date: day.date, Label: label, Amount: amount}
}

// buildCashOutDays returns the days with the heaviest outgoings, broken down
// by category, so "where did the money go?" has an answer without opening the
// entries table.
func buildCashOutDays(entries []domain.FinancialEntry) []CashOutDay {
	type acc struct {
		total int64
		items map[string]*CashOutItem
	}
	byDate := map[string]*acc{}

	for _, e := range entries {
		if e.Type != domain.EntryTypeExpense {
			continue
		}
		date := e.TransactionDate.String()
		bucket, ok := byDate[date]
		if !ok {
			bucket = &acc{items: map[string]*CashOutItem{}}
			byDate[date] = bucket
		}
		bucket.total += e.Amount
		item, ok := bucket.items[e.Category]
		if !ok {
			item = &CashOutItem{Category: e.Category}
			bucket.items[e.Category] = item
		}
		item.Amount += e.Amount
		item.Count++
	}

	days := make([]CashOutDay, 0, len(byDate))
	for date, bucket := range byDate {
		items := make([]CashOutItem, 0, len(bucket.items))
		for _, item := range bucket.items {
			items = append(items, *item)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Amount != items[j].Amount {
				return items[i].Amount > items[j].Amount
			}
			return items[i].Category < items[j].Category
		})
		days = append(days, CashOutDay{Date: date, Total: bucket.total, Items: items})
	}

	// Heaviest first; the date breaks ties so the top-N cut is stable rather
	// than dependent on map ordering.
	sort.Slice(days, func(i, j int) bool {
		if days[i].Total != days[j].Total {
			return days[i].Total > days[j].Total
		}
		return days[i].Date < days[j].Date
	})
	if len(days) > maxCashOutDays {
		days = days[:maxCashOutDays]
	}
	return days
}
