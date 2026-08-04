package analytics

import (
	"math"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// daysInWeek is spelled out because it is a divisor and a window length in
// several places here, never a magic 7.
const daysInWeek = 7

// within reports whether a calendar day falls inside an inclusive range.
// Compared as "YYYY-MM-DD" strings: the bounds and the date are calendar days
// with no time or zone of their own, and string order is the only comparison
// between them that cannot slide by a day.
func within(d, from, to domain.CalendarDate) bool {
	day := d.String()
	return day >= from.String() && day <= to.String()
}

// gaussianSigma is the standard deviation (in weeks) for the Gaussian weighting
// applied to the trailing 8-week window. A value of 2 means roughly 95% of the
// weight falls within ±2 weeks of the most recent week: recent weeks dominate,
// older ones fade, and a single unusual week moves the average by at most an
// eighth rather than a quarter.
const gaussianSigma = 2.0

// gaussianWeight returns the Gaussian weight for a given week offset from the
// most recent week. Offset 0 (most recent) yields 1.0; older weeks decay
// following exp(-0.5 * (offset/σ)²).
func gaussianWeight(weekOffset int) float64 {
	x := float64(weekOffset) / gaussianSigma
	return math.Exp(-0.5 * x * x)
}

// weekStart returns the Monday of the week containing d. Go's time.Weekday
// numbers Monday as 1, so (weekday-1) days back always lands on Monday.
// Sunday (0) wraps to 6 days back.
func weekStart(d domain.CalendarDate) domain.CalendarDate {
	t := d.Time()
	wd := int(t.Weekday())
	mondayOffset := wd - 1
	if wd == 0 {
		mondayOffset = 6
	}
	return domain.NewCalendarDate(t.AddDate(0, 0, -mondayOffset))
}

// weekOffsetFrom counts whole weeks between two Mondays: 0 for the window's
// most recent week, growing as the week gets older.
func weekOffsetFrom(endMonday, monday domain.CalendarDate) int {
	return int(endMonday.Time().Sub(monday.Time()).Hours() / 24 / 7)
}

// weekTotals is one total per week for a single day of the week, keyed by that
// week's Monday. Aggregating per (weekday, week) before averaging is the key
// invariant: two sales on one Monday count as one Monday, so a busy day does not
// outweigh a quiet one.
type weekTotals map[string]int64

// weekdayWeekTotals buckets entries by day of the week and, inside each, by the
// week they fall in. `pick` returns the date to bucket an entry on and whether
// to count it at all, so the caller owns both the window and the meaning: which
// entries are in, and which of their several dates the weekly rhythm should be
// read off.
//
// It is the caller's because the readings genuinely differ. The weekday card and
// the revenue projection bucket faturamento by the day of the *sale*
// (transaction basis); the cash runway buckets every inflow by the day the money
// *lands* (effective basis), because a runway is about the account balance and a
// sale on credit does not pay a bill.
//
// It also returns the window's most recent Monday, which is what the Gaussian
// weights are measured from — the two are derived together so a caller cannot
// weight one window's totals against another's anchor.
func weekdayWeekTotals(entries []domain.FinancialEntry, to domain.CalendarDate, pick func(domain.FinancialEntry) (domain.CalendarDate, bool)) ([daysInWeek]weekTotals, domain.CalendarDate) {
	var weeks [daysInWeek]weekTotals
	for i := range weeks {
		weeks[i] = weekTotals{}
	}
	endMonday := weekStart(to)
	for _, e := range entries {
		date, ok := pick(e)
		if !ok {
			continue
		}
		monday := weekStart(date)
		if offset := weekOffsetFrom(endMonday, monday); offset < 0 || offset >= projectionWindowWeeks {
			continue
		}
		weeks[int(date.Time().Weekday())][monday.String()] += e.Amount
	}
	return weeks, endMonday
}

// gaussianAvg collapses one weekday's per-week totals into the single figure a
// day of that weekday is worth, with recent weeks counting for more. A weekday
// no week ever traded stays at 0.
func gaussianAvg(weeks weekTotals, endMonday domain.CalendarDate) int64 {
	var weighted, weight float64
	for monday, total := range weeks {
		w := gaussianWeight(weekOffsetFrom(endMonday, dayFromStr(monday)))
		weighted += float64(total) * w
		weight += w
	}
	if weight == 0 {
		return 0
	}
	return roundToInt64(weighted / weight)
}

// weekdayStatsWeighted computes a Gaussian-weighted average of faturamento per
// day of the week over the trailing 8-week window. It replaces the former
// weekdayStats which only looked at the analysed month, because a month's own
// first days could not price the weekdays still to come and an unusual single
// week distorted the whole card.
//
// The Gaussian weighting (σ = 2 weeks) means recent weeks count more than
// older ones: a single holiday or one-off event two months ago barely moves
// the average, while a trend shift this week shows up almost immediately.
//
// Count is the number of distinct weeks that saw revenue for each weekday.
//
// Today is left out entirely: it is a day still being traded, and folding a
// morning's takings in as though it were a whole Tuesday drags that weekday's
// average down all day, further the earlier the analysis runs. It falls outside
// the window rather than being skipped here — see monthClock.projectionWindow.
func weekdayStatsWeighted(window []domain.FinancialEntry, from, to domain.CalendarDate, now time.Time) []WeekdayStat {
	today := int(now.Weekday())
	weeks, endMonday := weekdayWeekTotals(window, to, revenueOn(from, to))

	stats := make([]WeekdayStat, 0, daysInWeek)
	for d := 0; d < daysInWeek; d++ {
		count := len(weeks[d])
		var basis ProjectionBasis
		switch {
		case count == 0:
			basis = ProjectionNoBasis
		case count < daysInWeek:
			basis = ProjectionPartial
		default:
			basis = ProjectionFromWindow
		}
		stats = append(stats, WeekdayStat{
			Day:     d,
			Label:   weekdayLabels[d],
			Avg:     gaussianAvg(weeks[d], endMonday),
			Count:   count,
			IsToday: d == today,
			Basis:   basis,
		})
	}
	return stats
}

// dayFromStr parses a "YYYY-MM-DD" string back into a CalendarDate. It is used
// inside weekdayStatsWeighted to recover a date from a map key for weekOffset
// computation. The string always comes from CalendarDate.String(), so the parse
// cannot fail.
func dayFromStr(s string) domain.CalendarDate {
	t, _ := time.Parse("2006-01-02", s)
	return domain.NewCalendarDate(t)
}

// dailyRates prices each day of the week for the projection: what a Tuesday
// still to come is expected to bring.
type dailyRates struct {
	avg [daysInWeek]int64
	// sample is how many distinct dates traded inside the window — the evidence
	// the rates stand on, and the only thing that can honestly qualify them.
	// One (weekday, week) pair is one calendar date, so counting the pairs
	// counts the dates.
	sample int
}

// projectionRates averages faturamento per day of the week over the projection
// window, inclusive at both ends, with the same Gaussian weighting the weekday
// card is drawn from.
//
// The weighting used to stop at the card. The projection took a flat average of
// the window instead, so the page showed one Tuesday average and projected the
// month off a different one — and the flat figure was the worse of the two: a
// holiday or a one-off eight weeks ago priced next Tuesday exactly as heavily as
// last Tuesday did, and a genuine shift in trading took the full eight weeks to
// show up in the projection. Both readings now come from the same weighted
// averages (see weekdayStatsWeighted), so a day still to come is priced at what
// the card says that day of the week is worth.
//
// The rates used to come from the analysed month alone, which meant that early
// in a month most days of the week had never been seen and were priced at zero:
// on 3 August the twenty-one weekdays left projected nothing at all and the
// month came in at a quarter of its goal. A day of the week is a property of the
// pharmacy, not of the calendar month, so it is learned from the trailing weeks.
//
// A weekday that never traded across the whole window stays at zero: with
// projectionWindowWeeks chances observed, that is a day the pharmacy does not
// open, and inventing takings for it would overstate every projection.
//
// An unobserved weekday used to take the overall daily average whenever the
// window held fewer than seven trading days, on the theory that such a window
// knows nothing about the shape of a week. But the count of trading days cannot
// tell "a user in their first weeks" from "a shop that opens twice a week and
// closed for a holiday", and it made the projection lurch: a pharmacy trading
// only Saturdays, with six of eight in the window, priced all six other weekdays
// at a full Saturday — and the seventh Saturday, one more day of data, cut the
// projection sixfold. The overstating side was the one shown to whoever had the
// least data. A thin window now under-projects, which is the safe direction, and
// says so through Basis instead.
//
// window may carry days from outside the window; the range is applied here, so
// a caller that over-fetches cannot widen the average by accident.
func projectionRates(window []domain.FinancialEntry, from, to domain.CalendarDate) dailyRates {
	return ratesFromWeekTotals(weekdayWeekTotals(window, to, revenueOn(from, to)))
}

// revenueOn is the pick the weekday card and the revenue projection share: a
// sale, dated by the day it was made, inside the window.
func revenueOn(from, to domain.CalendarDate) func(domain.FinancialEntry) (domain.CalendarDate, bool) {
	return func(e domain.FinancialEntry) (domain.CalendarDate, bool) {
		return e.TransactionDate, domain.IsRevenue(e) && within(e.TransactionDate, from, to)
	}
}

// cashInRates is projectionRates for the cash runway: the same trailing window
// and the same weekly rhythm, but over every centavo that *lands*, bucketed by
// the day it lands on.
//
// It is deliberately not the revenue rates. A runway asks whether the account
// survives the week, and that turns on money arriving — a crediário sale made
// today pays no bill on Friday, while a loan or an aporte does. Feeding
// faturamento into the balance would credit the account with sales it has not
// been paid for.
//
// entries must be on the effective-date basis, the same basis the cash flow
// forecast buckets by, so the projection and the booked curve it is added to
// agree about which day a receipt belongs to.
func cashInRates(window []domain.FinancialEntry, from, to domain.CalendarDate) dailyRates {
	return ratesFromWeekTotals(weekdayWeekTotals(window, to, func(e domain.FinancialEntry) (domain.CalendarDate, bool) {
		if e.Type != domain.EntryTypeIncome {
			return domain.CalendarDate{}, false
		}
		d := domain.NewCalendarDate(pkgfinance.EffectiveDate(e))
		return d, within(d, from, to)
	}))
}

// ratesFromWeekTotals turns per-(weekday, week) totals into the rates a day
// still to come is priced at. It is shared by the revenue rates and the cash
// ones so that "recent weeks count for more, a weekday that never traded stays
// at zero, and a thin window under-projects rather than over-projects" is
// written once and cannot drift between the two.
func ratesFromWeekTotals(weeks [daysInWeek]weekTotals, endMonday domain.CalendarDate) dailyRates {
	var rates dailyRates
	for d := range weeks {
		rates.sample += len(weeks[d])
		rates.avg[d] = gaussianAvg(weeks[d], endMonday)
	}
	return rates
}

// basis reports what the projection is standing on, so the card and the bot can
// say so instead of presenting every projection with the same confidence.
//
// There is deliberately no "borrowed from earlier weeks" value: the window
// always reaches back past the analysed month, so such a caveat would fire every
// day of every month, and a warning that is always on is not read at all.
func (r dailyRates) basis() ProjectionBasis {
	switch {
	case r.sample == 0:
		return ProjectionNoBasis
	case r.sample < daysInWeek:
		return ProjectionPartial
	default:
		return ProjectionFromWindow
	}
}

// The faturamento predicate used to live here as a private twin of
// packages/finance.IsFaturamento, kept in sync by hand because this package
// could not import the other's helpers back without a cycle. Both are gone:
// the rule is now a field on the entry and the predicate is
// domain.IsRevenue, which both packages can reach.
