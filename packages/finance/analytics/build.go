package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// HistoryMonths is the length of the trailing window the history chart and the
// month-over-month trends read from, ending at the month being analysed.
const HistoryMonths = 3

// Input is everything Build needs. Assemble fills it from a Store; the split
// exists so the analysis itself stays a pure function that tests can drive
// without a store or a clock.
type Input struct {
	// Month is the month under analysis, "YYYY-MM".
	Month string
	// Entries are the analysed month's entries; PreviousEntries the month
	// before's. Both are needed in full because the month-over-month
	// comparison re-totals them day by day rather than reading the stored
	// summaries, which only exist for whole months.
	Entries         []domain.FinancialEntry
	PreviousEntries []domain.FinancialEntry
	// Summaries and Goals run oldest-first over the trailing HistoryMonths
	// window ending at Month, so the analysed month is the *last* slot. A month
	// with no data is a nil hole rather than a shorter slice — collapsing it
	// would silently shift every other month onto the wrong label.
	Summaries      []*pkgfinance.MonthlySummary
	Goals          []*domain.Goal
	CashFlowPoints []pkgfinance.CashFlowPoint
	// Now anchors "today", "this week" and "days remaining". It must already
	// be in the user's timezone: every date below is read off its calendar
	// fields, so passing a UTC instant makes the analysis wrong by a day for
	// part of each evening in Brazil.
	Now time.Time
}

// Build assembles the full analysis from already-fetched data.
func Build(in Input) Analysis {
	months := MonthRange(in.Month, HistoryMonths)
	current := len(months) - 1

	currentSummary := pkgfinance.MonthlySummary{Month: in.Month}
	if s := at(in.Summaries, current); s != nil {
		currentSummary = *s
	}
	currentGoal := at(in.Goals, current)

	income := incomeTotal(in.Entries)

	kpis := KPIs{
		Resultado:                  currentSummary.Balance,
		Receita:                    currentSummary.TotalIncome,
		Despesa:                    currentSummary.TotalExpense,
		DaysRemaining:              daysInMonth(in.Now) - in.Now.Day(),
		PreviousMonthIncomeUpToDay: incomeUpToDay(in.PreviousEntries, in.Now.Day()),
	}

	var incomeTarget int64
	if currentGoal != nil {
		incomeTarget = currentGoal.IncomeTarget
	}
	week := buildWeekComparison(in.Entries, in.Now, incomeTarget)
	goals := goalProgress(currentSummary, currentGoal, in.Now, income)
	weekdays := weekdayStats(in.Entries, in.Now)
	// One projection of the month, and one per-day ask derived from it, shared
	// by the health insight, the weekly recommendation, the dashboard card and
	// the bot — they each used to work one out for themselves and disagreed.
	projection := buildProjection(weekdays, goals, in.Now)
	// One month-over-month comparison, measured at the same height of both
	// months, shared by the trends and the health insights — they used to
	// derive it separately from the full summaries and both inherited the
	// partial-month distortion.
	compared := buildComparison(in.Month, in.Entries, in.PreviousEntries, in.Now)
	trends := buildTrends(compared)
	cashPosition := buildCashPosition(in.CashFlowPoints, in.Now)

	return Analysis{
		Month:              in.Month,
		KPIs:               kpis,
		Health:             buildHealth(in.Entries, currentSummary, compared, week, projection),
		Trends:             trends,
		Weekdays:           weekdays,
		WeekComparison:     week,
		Highlights:         buildHighlights(in.Entries),
		CashOutDays:        buildCashOutDays(in.Entries),
		ExpenseComposition: expenseComposition(in.Entries),
		Goals:              goals,
		Projection:         projection,
		History:            buildHistory(months, in.Summaries, in.Goals),
		CashPosition:       cashPosition,
		Recommendations:    buildRecommendations(week, projection, trends, cashPosition),
	}
}

// buildWeekComparison measures income from this Monday through today against
// the whole of last week, and projects the rest of *this week* from the
// resulting daily rate. Projecting the month is buildProjection's job, off
// the weekday averages — this used to do both and the two disagreed.
//
// Comparisons are done on "YYYY-MM-DD" strings rather than instants: the week
// boundaries come from now's calendar fields, and an entry's date is a
// calendar day with no time or zone of its own, so string ordering is the only
// comparison that cannot slide a day.
func buildWeekComparison(entries []domain.FinancialEntry, now time.Time, monthlyTarget int64) WeekComparison {
	dayOfWeek := int(now.Weekday()) // 0 = Sunday
	// Weeks run Monday-to-Sunday here, so Sunday belongs to the week that
	// started six days ago, not to the one starting tomorrow.
	mondayOffset := 1 - dayOfWeek
	if dayOfWeek == 0 {
		mondayOffset = -6
	}
	thisMonday := now.AddDate(0, 0, mondayOffset)

	thisMondayStr := thisMonday.Format("2006-01-02")
	lastMondayStr := thisMonday.AddDate(0, 0, -7).Format("2006-01-02")
	lastSundayStr := thisMonday.AddDate(0, 0, -1).Format("2006-01-02")
	todayStr := now.Format("2006-01-02")

	// The same weekday as today, one week back — the fair mid-week comparison.
	sameDayOffset := dayOfWeek - 1
	if dayOfWeek == 0 {
		sameDayOffset = 6
	}
	lastSameDayStr := thisMonday.AddDate(0, 0, -7+sameDayOffset).Format("2006-01-02")

	week := WeekComparison{MonthlyTarget: monthlyTarget}
	for _, e := range entries {
		if !isIncome(e) {
			continue
		}
		date := e.TransactionDate.String()
		switch {
		case date >= thisMondayStr && date <= todayStr:
			week.Current += e.Amount
		case date >= lastMondayStr && date <= lastSundayStr:
			week.Previous += e.Amount
		}
		if date >= lastMondayStr && date <= lastSameDayStr {
			week.PreviousUpToDay += e.Amount
		}
	}

	// One label per day of the current week that has actually happened, so the
	// chart never draws a bar for a day that has not been traded yet.
	labels := make([]string, 0, 7)
	for i := 0; i < 7; i++ {
		d := thisMonday.AddDate(0, 0, i)
		if d.Format("2006-01-02") > todayStr {
			break
		}
		labels = append(labels, weekdayLabels[int(d.Weekday())])
	}
	week.Labels = labels

	// Project the rest of this week at last week's daily average.
	avgPerDay := float64(week.Previous) / 7
	remainingDays := 7 - dayOfWeek
	if dayOfWeek == 0 {
		remainingDays = 0
	}
	week.ProjectedWeekly = roundToInt64(float64(week.Current) + avgPerDay*float64(remainingDays))

	return week
}

// MonthRange returns the trailing window of `count` months ending at month
// (inclusive), oldest-first. An unparseable month yields just that month, so
// callers still get a usable window rather than an empty one.
func MonthRange(month string, count int) []string {
	t, _, err := domain.ParseMonth(month)
	if err != nil {
		return []string{month}
	}
	months := make([]string, 0, count)
	for i := count - 1; i >= 0; i-- {
		months = append(months, domain.MonthOf(t.AddDate(0, -i, 0)))
	}
	return months
}

// incomeTotal sums income across the given entries.
func incomeTotal(entries []domain.FinancialEntry) int64 {
	var total int64
	for _, e := range entries {
		if isIncome(e) {
			total += e.Amount
		}
	}
	return total
}

// incomeUpToDay sums income falling on or before the given day of the
// month — how last month is truncated for a like-for-like comparison with a
// month still in progress.
func incomeUpToDay(entries []domain.FinancialEntry, day int) int64 {
	var total int64
	for _, e := range entries {
		if isIncome(e) && e.TransactionDate.Day() <= day {
			total += e.Amount
		}
	}
	return total
}

// at returns the slice element at i, or nil when i is out of range — the
// trailing window is positional, so a short slice is a hole, not an error.
func at[T any](s []*T, i int) *T {
	if i < 0 || i >= len(s) {
		return nil
	}
	return s[i]
}
