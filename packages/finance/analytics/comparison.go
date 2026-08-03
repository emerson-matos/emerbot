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

// comparison holds two readings of the analysed month that are deliberately
// taken over different windows, because they answer different questions.
//
// realized is everything that has finished here — every closed day of the
// month. It is what the traffic light, the result and the expense ratio stand
// on: facts about this month that need no counterpart to mean something.
//
// current and previous are the two sides of the month-over-month comparison,
// and they stop at the last *whole week* of the month rather than at the last
// closed day (see monthClock.comparableThrough). A percentage needs both months
// to have been sampled the same way; a bare total does not.
//
// Keeping them apart is what lets the first week of a month say "three of five
// days closed in the black" while saying nothing at all about last month.
type comparison struct {
	realized monthTotals
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
// whole. A month on its first day has no finished day at all: everything stays
// zero and clock.measurable() reports false, so every consumer says "not yet"
// instead of quoting a percentage of nothing.
//
// Truncating both sides at the same day number is still not enough to make the
// two comparable, and the residue is worst in the first week: days 1–2 of
// August 2026 are a Saturday and a Sunday, days 1–2 of July a Wednesday and a
// Thursday. So the comparison window closes on whole weeks — see
// monthClock.comparableThrough — and the first week of a month has no
// month-over-month reading at all, exactly as its first day has none.
//
// currentRevenue/previousRevenue are the same two months read on the
// transaction basis; they fill monthTotals.revenue, which is bucketed by the
// day of the sale rather than the effective date.
func buildComparison(clock monthClock, current, previous, currentRevenue, previousRevenue []domain.FinancialEntry) comparison {
	c := comparison{clock: clock}
	if clock.measurable() {
		c.realized = totalsThroughDay(current, clock.through)
		c.realized.revenue = revenueThroughDay(currentRevenue, clock.through)
	}
	if window := clock.comparableThrough(); window > 0 {
		c.current = totalsThroughDay(current, window)
		c.current.revenue = revenueThroughDay(currentRevenue, window)
		c.previous = totalsThroughDay(previous, window)
		c.previous.revenue = revenueThroughDay(previousRevenue, window)
	}
	return c
}

// window is the last day of the month both sides of the comparison were
// measured through, and comparable reports whether there is one at all. It is
// derived from the clock rather than stored so a comparison can never carry a
// window that disagrees with the month it was built for.
func (c comparison) window() int { return c.clock.comparableThrough() }

func (c comparison) comparable() bool { return c.window() > 0 }

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

// windowSuffix names the days a month-over-month figure was measured over, so a
// percentage never reads as a whole-month one when it is only the month so far.
// Empty for two closed months, where "vs mês passado" already says it all.
//
// realizedSuffix does the same for a figure about this month alone. The two
// differ during a month — the result is stated through yesterday while the
// comparison stops at the last whole week — and they are separate methods
// because a line that quotes the wrong one is a line that misstates its own
// window, which is the failure this package keeps having.
func (c comparison) windowSuffix() string { return c.suffixFor(c.window()) }

func (c comparison) realizedSuffix() string { return c.suffixFor(c.clock.through) }

func (c comparison) suffixFor(day int) string {
	if !c.clock.inProgress {
		return ""
	}
	return fmt.Sprintf(" (até o dia %d)", day)
}
