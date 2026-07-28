package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// weekdayStats averages income per day of the week across the analysed
// month. The average divides by the number of distinct *dates* that saw
// income, not by the number of entries — three sales on one Tuesday are one
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
		if !isIncome(e) {
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

// isIncome reports whether an entry counts as receita (income) — money earned
// by selling something, as opposed to money that merely moved, like an
// expense payment. It is what the goals, projections, weekday averages,
// week-over-week pace and day highlights are all measured against.
//
// Today every income category (venda_balcao, convenio, delivery,
// outros_receitas) is a sale, so this is simply "is it income". If a
// non-operational income category is ever added — a loan disbursement, a
// partner contribution, an investment redemption — it must be excluded here:
// cash coming in is not the same thing as receita, and conflating the two is
// exactly the "empréstimo não é faturamento" mistake this function exists to
// prevent.
func isIncome(e domain.FinancialEntry) bool {
	return e.Type == domain.EntryTypeIncome
}
