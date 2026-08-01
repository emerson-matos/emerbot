package analytics

import (
	"fmt"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// monthTotals is a month's totals, possibly measured only up to a day of the
// month.
//
// revenue and income are separate on purpose. revenue is faturamento — what was
// sold — and is what the performance trend reports. income is every inflow that
// falls in the month, and is what balance is made of: narrowing income to sales
// would turn balance into "sales minus every expense", quietly reporting a loss
// for a month that a loan or an aporte actually covered.
type monthTotals struct {
	revenue int64
	income  int64
	expense int64
	balance int64
}

// comparison holds the two sides of a month-over-month comparison, measured
// over the same finished days of both months.
type comparison struct {
	current  monthTotals
	previous monthTotals
	clock    monthClock
}

// buildComparison measures the analysed month and the one before it over the
// same *finished* days, so a month in progress is never held against a month
// that already ran its course.
//
// Comparing an unfinished July against a closed June always "shows a fall": on
// the 5th, five days of income are being divided by thirty. That made the trend
// arrows point down every month, and fired "Receita caiu" recommendations at
// the start of every month — an alert that cries wolf monthly teaches people to
// ignore the page.
//
// Both sides stop at *yesterday*, not at today. Truncating at today's day
// number fixed the whole-month distortion but left a smaller one that is worst
// exactly when the digest goes out: at 9am today has barely been traded, and it
// was being weighed against the same date last month in full. On the 1st the
// two effects met — a single empty morning against a whole trading day — and
// the digest opened with "queda de 100% na receita" before the shop had opened.
//
// A month that is not the one now falls in is already closed and is compared
// whole. A month on its first day has no finished day at all: both sides stay
// zero and clock.measurable() reports false, so every consumer says "not yet"
// instead of quoting a percentage of nothing.
//
// currentRevenue/previousRevenue are the same two months read on the
// transaction basis; they fill monthTotals.revenue, which is bucketed by the
// day of the sale rather than the effective date.
func buildComparison(clock monthClock, current, previous, currentRevenue, previousRevenue []domain.FinancialEntry) comparison {
	if !clock.measurable() {
		return comparison{clock: clock}
	}
	cur := totalsThroughDay(current, clock.through)
	prev := totalsThroughDay(previous, clock.through)
	cur.revenue = revenueThroughDay(currentRevenue, clock.through)
	prev.revenue = revenueThroughDay(previousRevenue, clock.through)
	return comparison{current: cur, previous: prev, clock: clock}
}

// totalsThroughDay sums entries falling on or before throughDay of their month.
//
// Entries are bucketed by their effective date, the same field the stored
// monthly summaries use — bucketing by anything else here would make the
// comparison disagree with the KPIs shown right next to it.
func totalsThroughDay(entries []domain.FinancialEntry, throughDay int) monthTotals {
	var totals monthTotals
	for _, e := range entries {
		if pkgfinance.EffectiveDate(e).Day() > throughDay {
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

// revenueThroughDay sums faturamento up to throughDay, bucketed by the day of
// the sale — the only date a sale can honestly be attributed to. entries must
// have been read on the transaction basis.
func revenueThroughDay(entries []domain.FinancialEntry, throughDay int) int64 {
	var total int64
	for _, e := range entries {
		if !domain.IsRevenue(e) || e.TransactionDate.Day() > throughDay {
			continue
		}
		total += e.Amount
	}
	return total
}

// revenueOnDay sums the faturamento booked on one day of the analysed month.
// The projection needs it to avoid counting today twice — once as money already
// in, once as a day still expected to bring its weekday average.
func revenueOnDay(entries []domain.FinancialEntry, day int) int64 {
	var total int64
	for _, e := range entries {
		if domain.IsRevenue(e) && e.TransactionDate.Day() == day {
			total += e.Amount
		}
	}
	return total
}

// suffix names the window the comparison actually measured, so a percentage
// never reads as a whole-month figure when it is only the month so far.
// Empty for two closed months, where "vs mês passado" already says it all.
func (c comparison) suffix() string {
	if !c.clock.inProgress {
		return ""
	}
	return fmt.Sprintf(" (até o dia %d)", c.clock.through)
}
