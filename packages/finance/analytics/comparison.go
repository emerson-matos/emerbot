package analytics

import (
	"fmt"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// monthTotals is a month's income, expense and balance, possibly measured only
// up to a day of the month.
type monthTotals struct {
	income  int64
	expense int64
	balance int64
}

// comparison holds the two sides of a month-over-month comparison, measured at
// the same height of the month, plus the day both were cut at.
type comparison struct {
	current  monthTotals
	previous monthTotals
	// throughDay is the day of the month both sides were truncated at, or 0
	// when both months are closed and compared whole.
	throughDay int
}

// buildComparison measures the analysed month and the one before it up to the
// same day, so a month in progress is never held against a month that already
// finished.
//
// Comparing an unfinished July against a closed June always "shows a fall":
// on the 5th, five days of income are being divided by thirty. That made the
// trend arrows point down every month, and fired "Receita caiu" recommendations
// at the start of every month — an alert that cries wolf monthly teaches people
// to ignore the page. Truncating both sides at today's day number is the
// comparison a person actually means by "vs mês passado".
//
// Both sides are truncated, not just the previous one: an entry due later this
// month already counts toward this month's total (see finance.EffectiveDate),
// so leaving the current side whole would tilt the comparison the other way.
//
// A month that is not the one now falls in is already closed, and is compared
// whole (throughDay 0) — truncating a finished month at today's day number
// would be arbitrary.
func buildComparison(month string, current, previous []domain.FinancialEntry, now time.Time) comparison {
	throughDay := 0
	if now.Format("2006-01") == month {
		throughDay = now.Day()
	}
	return comparison{
		current:    totalsThroughDay(current, throughDay),
		previous:   totalsThroughDay(previous, throughDay),
		throughDay: throughDay,
	}
}

// totalsThroughDay sums entries falling on or before throughDay of their
// month; throughDay <= 0 sums the whole month.
//
// Entries are bucketed by their effective date, the same field the stored
// monthly summaries use — bucketing by anything else here would make the
// comparison disagree with the KPIs shown right next to it.
func totalsThroughDay(entries []domain.FinancialEntry, throughDay int) monthTotals {
	var totals monthTotals
	for _, e := range entries {
		if throughDay > 0 && pkgfinance.EffectiveDate(e).Day() > throughDay {
			continue
		}
		if e.Type == domain.EntryTypeIncome {
			totals.income += e.Amount
		} else {
			totals.expense += e.Amount
		}
	}
	totals.balance = totals.income - totals.expense
	return totals
}

// suffix names the window the comparison actually measured, so a percentage
// never reads as a whole-month figure when it is only the month so far.
// Empty for two closed months, where "vs mês passado" already says it all.
func (c comparison) suffix() string {
	if c.throughDay <= 0 {
		return ""
	}
	return fmt.Sprintf(" (até o dia %d)", c.throughDay)
}
