package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// weekdayStats averages counter sales per day of the week across the analysed
// month. The average divides by the number of distinct *dates* that saw a
// sale, not by the number of entries — three sales on one Tuesday are one
// Tuesday, otherwise a busy day would drag the average down.
func weekdayStats(entries []domain.FinancialEntry, now time.Time) []WeekdayStat {
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
		if !isCounterSale(e) {
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

// isCounterSale reports whether an entry is a counter sale (venda_balcao
// income) — the only revenue the goals, weekday averages and week-over-week
// pace are measured against, since that is what the pharmacy actually
// controls day to day.
func isCounterSale(e domain.FinancialEntry) bool {
	return e.Type == domain.EntryTypeIncome && e.Category == "venda_balcao"
}
