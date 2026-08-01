package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// weekdayStats averages faturamento per day of the week across the analysed
// month. entries must be on the transaction basis (see finance.DateBasis).
//
// The average divides by the number of distinct *dates* that saw
// faturamento, not by the number of entries — three sales on one Tuesday are
// one Tuesday, otherwise a busy day would drag the average down.
//
// Today is left out entirely: it is a day still being traded, and folding a
// morning's takings in as though it were a whole Tuesday drags that weekday's
// average — and the projection built on it — down all day, further the earlier
// the analysis runs.
func weekdayStats(entries []domain.FinancialEntry, now time.Time, clock monthClock) []WeekdayStat {
	today := int(now.Weekday())

	type bucket struct {
		total int64
		dates map[string]struct{}
	}
	buckets := make([]bucket, 7)
	for i := range buckets {
		buckets[i].dates = map[string]struct{}{}
	}

	for _, e := range entries {
		if !domain.IsRevenue(e) || e.TransactionDate.Day() > clock.through {
			continue
		}
		date := e.TransactionDate.Time()
		b := &buckets[int(date.Weekday())]
		b.total += e.Amount
		b.dates[e.TransactionDate.String()] = struct{}{}
	}

	stats := make([]WeekdayStat, 0, 7)
	for d := 0; d < 7; d++ {
		count := len(buckets[d].dates)
		var avg int64
		if count > 0 {
			avg = roundToInt64(float64(buckets[d].total) / float64(count))
		}
		stats = append(stats, WeekdayStat{
			Day:     d,
			Label:   weekdayLabels[d],
			Avg:     avg,
			Total:   buckets[d].total,
			Count:   count,
			IsToday: d == today,
		})
	}
	return stats
}

// The faturamento predicate used to live here as a private twin of
// packages/finance.IsFaturamento, kept in sync by hand because this package
// could not import the other's helpers back without a cycle. Both are gone:
// the rule is now a field on the entry and the predicate is
// domain.IsRevenue, which both packages can reach.
