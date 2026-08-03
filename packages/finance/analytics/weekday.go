package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// daysInWeek is spelled out because it is a divisor and a window length in
// several places here, never a magic 7.
const daysInWeek = 7

// weekdayBucket accumulates faturamento for one day of the week. dates is a set
// rather than a counter because the average divides by the number of distinct
// *dates* that saw faturamento, not by the number of entries — three sales on
// one Tuesday are one Tuesday, otherwise a busy day would drag the average down.
type weekdayBucket struct {
	total int64
	dates map[string]struct{}
}

// weekdayBuckets buckets faturamento by day of the week, keeping only the
// entries whose transaction date `keep` accepts. The predicate is the caller's
// because the two readings want different windows out of the same shape: the
// dashboard's weekday card is about the analysed month, the projection about
// the trailing weeks.
//
// entries must be on the transaction basis (see finance.DateBasis).
func weekdayBuckets(entries []domain.FinancialEntry, keep func(domain.CalendarDate) bool) [daysInWeek]weekdayBucket {
	var buckets [daysInWeek]weekdayBucket
	for i := range buckets {
		buckets[i].dates = map[string]struct{}{}
	}
	for _, e := range entries {
		if !domain.IsRevenue(e) || !keep(e.TransactionDate) {
			continue
		}
		b := &buckets[int(e.TransactionDate.Time().Weekday())]
		b.total += e.Amount
		b.dates[e.TransactionDate.String()] = struct{}{}
	}
	return buckets
}

// avg is the bucket's faturamento per day it traded, 0 for a weekday that never
// did.
func (b weekdayBucket) avg() int64 {
	if len(b.dates) == 0 {
		return 0
	}
	return roundToInt64(float64(b.total) / float64(len(b.dates)))
}

// weekdayStats averages faturamento per day of the week across the analysed
// month. It is the "Média por Dia da Semana" card, and it is deliberately about
// *this month only* — the empty state reads "neste mês", and a day of the week
// the pharmacy has not sold on yet must show a dash rather than borrow a figure
// from an earlier week. The projection, which cannot afford that dash, has its
// own window: see projectionRates.
//
// Today is left out entirely: it is a day still being traded, and folding a
// morning's takings in as though it were a whole Tuesday drags that weekday's
// average down all day, further the earlier the analysis runs.
func weekdayStats(entries []domain.FinancialEntry, now time.Time, clock monthClock) []WeekdayStat {
	today := int(now.Weekday())
	// The month is checked, not just the day number. Callers pass entries
	// already scoped to the analysed month, so this was latent — but "day of the
	// month <= through" alone lets 1 July into an August card whenever August has
	// reached its 2nd, and the card's whole claim is that it is about one month.
	buckets := weekdayBuckets(entries, func(d domain.CalendarDate) bool {
		return d.Year() == clock.first.Year() && d.Month() == clock.first.Month() &&
			d.Day() <= clock.through
	})

	stats := make([]WeekdayStat, 0, daysInWeek)
	for d := 0; d < daysInWeek; d++ {
		stats = append(stats, WeekdayStat{
			Day:     d,
			Label:   weekdayLabels[d],
			Avg:     buckets[d].avg(),
			Total:   buckets[d].total,
			Count:   len(buckets[d].dates),
			IsToday: d == today,
		})
	}
	return stats
}

// dailyRates prices each day of the week for the projection: what a Tuesday
// still to come is expected to bring.
type dailyRates struct {
	avg [daysInWeek]int64
	// sample is how many distinct dates traded inside the window — the evidence
	// the rates stand on, and the only thing that can honestly qualify them.
	sample int
}

// projectionRates averages faturamento per day of the week over the projection
// window, inclusive at both ends.
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
	// Compared as "YYYY-MM-DD" strings: the bounds and the entry are calendar
	// days with no time or zone of their own, and string order is the only
	// comparison between them that cannot slide by a day.
	start, end := from.String(), to.String()
	buckets := weekdayBuckets(window, func(d domain.CalendarDate) bool {
		day := d.String()
		return day >= start && day <= end
	})

	var rates dailyRates
	for d := range buckets {
		rates.sample += len(buckets[d].dates)
		rates.avg[d] = buckets[d].avg()
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
