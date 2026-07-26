package analytics

import (
	"sort"

	"github.com/emerson/emerbot/packages/domain"
)

// maxCashOutDays caps how many heavy-spending days the analysis reports.
const maxCashOutDays = 5

type dayTotals struct {
	date    string
	income  int64
	expense int64
	balance int64
}

// aggregateByDay folds entries into per-day totals, ordered oldest-first.
//
// Anything that is not a counter sale counts against the day, income included:
// a supplier refund or a transfer is money moving, not a day's takings, and
// letting it inflate "best day" would make the highlight meaningless.
func aggregateByDay(entries []domain.FinancialEntry) []dayTotals {
	byDate := map[string]*dayTotals{}
	for _, e := range entries {
		date := e.TransactionDate.String()
		day, ok := byDate[date]
		if !ok {
			day = &dayTotals{date: date}
			byDate[date] = day
		}
		if isCounterSale(e) {
			day.income += e.Amount
		} else {
			day.expense += e.Amount
		}
		day.balance = day.income - day.expense
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

// buildHighlights picks the best and worst day of the month by revenue and by
// balance. With no entries at all every slot is the same "Sem dados"
// placeholder, so the dashboard renders empty cards instead of a zero-value
// date.
func buildHighlights(entries []domain.FinancialEntry) Highlights {
	days := aggregateByDay(entries)
	if len(days) == 0 {
		empty := DayHighlight{Date: NoDataDate, Label: "Sem dados", Amount: 0}
		return Highlights{BestIncome: empty, WorstIncome: empty, BestBalance: empty, WorstBalance: empty}
	}

	bestIncome, worstIncome := days[0], days[0]
	bestBalance, worstBalance := days[0], days[0]
	for _, d := range days[1:] {
		if d.income > bestIncome.income {
			bestIncome = d
		}
		if d.income < worstIncome.income {
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
		BestIncome:   toHighlight(bestIncome, bestIncome.income),
		WorstIncome:  toHighlight(worstIncome, worstIncome.income),
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
