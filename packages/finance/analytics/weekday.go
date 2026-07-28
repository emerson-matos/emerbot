package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// weekdayStats averages faturamento per day of the week across the analysed
// month. The average divides by the number of distinct *dates* that saw
// faturamento, not by the number of entries — three sales on one Tuesday are
// one Tuesday, otherwise a busy day would drag the average down.
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
		if !isFaturamento(e) {
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

// outrosReceitasCategory is the one income category that is not unambiguously
// a sale — the catch-all a person reaches for when nothing else fits. That
// makes it the one place a loan disbursement, a partner contribution or an
// investment redemption is likely to get filed, for lack of a better bucket.
const outrosReceitasCategory = "outros_receitas"

// isFaturamento reports whether an entry counts toward faturamento — revenue
// earned by selling something, as opposed to money that merely moved (an
// expense payment) or cash that came in without a sale behind it (a loan, an
// aporte, an investment redemption). It is what the goals, projections,
// weekday averages, week-over-week pace and day highlights are all measured
// against.
//
// venda_balcao, convenio and delivery are unambiguous sales. outros_receitas
// is excluded on purpose: a pharmacy's goal is "quanto vendemos", and a loan
// or capital contribution logged under "Outros (Receita)" for lack of a
// better category must not count toward it — that is exactly the
// "empréstimo não é faturamento" mistake this function exists to prevent.
// KPIs.Receita and Trends.Receita are the broader figure, deliberately
// including outros_receitas: they answer "how much came in", not "how much
// did we sell".
func isFaturamento(e domain.FinancialEntry) bool {
	return e.Type == domain.EntryTypeIncome && e.Category != outrosReceitasCategory
}
