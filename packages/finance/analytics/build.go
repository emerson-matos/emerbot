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
	// Entries are the analysed month's entries on the effective-date basis;
	// PreviousEntries the month before's. Both are needed in full because the
	// month-over-month comparison re-totals them day by day rather than reading
	// the stored summaries, which only exist for whole months.
	//
	// These feed everything that is *not* faturamento: expense composition,
	// cash-out days, "dias com movimento".
	Entries         []domain.FinancialEntry
	PreviousEntries []domain.FinancialEntry
	// RevenueEntries are the same two months read on the transaction-date
	// basis, and they feed every faturamento figure — the goal, the projection,
	// the weekday averages, the week-over-week pace, the day highlights.
	//
	// They are a separate read rather than a filter over Entries because the
	// two sets genuinely differ: a crediário sale made in the analysed month
	// but due in the next one is in RevenueEntries and not in Entries. Deriving
	// revenue from Entries is how the KPI and the goal ended up disagreeing
	// about which month a sale belonged to.
	RevenueEntries         []domain.FinancialEntry
	PreviousRevenueEntries []domain.FinancialEntry
	// WindowRevenueEntries are faturamento over the projection's trailing
	// window (monthClock.projectionWindow), also on the transaction-date basis.
	// It reaches further back than PreviousRevenueEntries and is not aligned to
	// a month, which is the point: a day of the week is a property of the
	// pharmacy, not of the calendar month, and the month's own first days cannot
	// price the ones still to come.
	//
	// It may carry days from outside the window — projectionRates applies the
	// range itself, so an over-fetching caller cannot widen the averages.
	WindowRevenueEntries []domain.FinancialEntry
	// WindowEntries is the same window read on the effective-date basis, and it
	// prices the cash runway. It is a separate read from WindowRevenueEntries
	// for the same reason Entries is separate from RevenueEntries: a runway is
	// about money landing, so it counts every inflow and dates it by the day it
	// arrives, while faturamento counts sales and dates them by the day they
	// were made. Using one for the other credits the account with crediário it
	// has not been paid.
	WindowEntries []domain.FinancialEntry
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

	// Where we stand inside the month, decided once. Everything retrospective
	// below stops at clock.through (yesterday) and everything forward-looking
	// starts at clock.today — see monthClock for why the two used to be swapped.
	clock := newMonthClock(in.Month, in.Now)

	currentSummary := pkgfinance.MonthlySummary{Month: in.Month}
	if s := at(in.Summaries, current); s != nil {
		currentSummary = *s
	}
	currentGoal := at(in.Goals, current)

	// Faturamento comes off the summary, which reads it on the transaction
	// basis — the same basis RevenueEntries below is read on, so every revenue
	// figure in this Analysis agrees about which month a sale falls in.
	// EntradasCaixa is the broader figure and is deliberately different: it is
	// what arrived, not what was sold.
	faturamento := currentSummary.TotalRevenue

	kpis := KPIs{
		Resultado:     currentSummary.ExpectedBalance,
		Faturamento:   faturamento,
		EntradasCaixa: currentSummary.TotalCashIn,
		Despesa:       currentSummary.TotalExpense,
	}

	var revenueTarget int64
	if currentGoal != nil {
		revenueTarget = currentGoal.RevenueTarget
	}
	week := buildWeekComparison(in.RevenueEntries, in.Now, revenueTarget)
	goals := goalProgress(currentSummary, currentGoal, clock, faturamento)
	weekdays := weekdayStats(in.RevenueEntries, in.Now, clock)
	// One projection of the month, and one per-day ask derived from it, shared
	// by the health insight, the weekly recommendation, the dashboard card and
	// the bot — they each used to work one out for themselves and disagreed.
	//
	// The rates come from the trailing window rather than from `weekdays`, which
	// is the card and stays about this month alone: reading the month's own
	// finished days priced every weekday it had not reached yet at zero, so the
	// projection was worth least on the days it was consulted most.
	windowFrom, windowTo := clock.projectionWindow()
	rates := projectionRates(in.WindowRevenueEntries, windowFrom, windowTo)
	projection := buildProjection(rates, goals, in.Now, clock, revenueOnDay(in.RevenueEntries, clock.today))
	// One month-over-month comparison, measured over the same finished days of
	// both months, shared by the trends and the health insights — they used to
	// derive it separately from the full summaries and both inherited the
	// partial-month distortion.
	compared := buildComparison(clock, in.Entries, in.PreviousEntries, in.RevenueEntries, in.PreviousRevenueEntries)
	trends := buildTrends(compared)
	// The runway reads the same trailing window, on the cash basis: the booked
	// curve carries the whole month's bills from the 1st and none of its sales,
	// so left alone it reports a balance going negative within days every time a
	// month opens.
	cashPosition := buildCashPosition(in.CashFlowPoints, cashInRates(in.WindowEntries, windowFrom, windowTo), in.Now)

	return Analysis{
		Schema:             SchemaVersion,
		Month:              in.Month,
		Period:             clock.period(),
		KPIs:               kpis,
		Health:             buildHealth(in.Entries, compared, week, projection),
		Trends:             trends,
		Weekdays:           weekdays,
		WeekComparison:     week,
		Highlights:         buildHighlights(in.Entries, in.RevenueEntries),
		CashOutDays:        buildCashOutDays(in.Entries),
		ExpenseComposition: expenseComposition(in.Entries),
		Goals:              goals,
		Projection:         projection,
		History:            buildHistory(months, in.Summaries, in.Goals),
		CashPosition:       cashPosition,
		Recommendations:    buildRecommendations(week, projection, trends, cashPosition, compared),
	}
}

// buildWeekComparison measures faturamento from this Monday through today
// against the whole of last week, and projects the rest of *this week* from
// the resulting daily rate. Projecting the month is buildProjection's job, off
// the weekday averages — this used to do both and the two disagreed.
//
// Alongside those totals it fills Pace, the pair the insights and the
// recommendation actually read: this week and last week over the same
// *finished* days of the week, Monday through yesterday on both sides. The
// pace used to be this week including today against last week's matching
// weekday in full, which is the same partial-day distortion buildComparison
// carries at the month level — a Monday morning read as "ritmo caiu 100%".
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

	// Days of this week that have finished: 0 on a Monday, 6 on a Sunday. Both
	// halves of the pace cover exactly this many days.
	finished := dayOfWeek - 1
	if dayOfWeek == 0 {
		finished = 6
	}
	// Empty windows when nothing has finished — no date can fall inside them,
	// so a Monday reports no pace at all rather than a fall against a full day.
	paceEnd, lastPaceEnd := "", ""
	if finished > 0 {
		paceEnd = thisMonday.AddDate(0, 0, finished-1).Format("2006-01-02")
		lastPaceEnd = thisMonday.AddDate(0, 0, -7+finished-1).Format("2006-01-02")
	}

	week := WeekComparison{MonthlyTarget: monthlyTarget, Pace: WeekPace{Days: finished}}
	for _, e := range entries {
		if !domain.IsRevenue(e) {
			continue
		}
		date := e.TransactionDate.String()
		switch {
		case date >= thisMondayStr && date <= todayStr:
			week.Current += e.Amount
		case date >= lastMondayStr && date <= lastSundayStr:
			week.Previous += e.Amount
		}
		if finished > 0 {
			if date >= thisMondayStr && date <= paceEnd {
				week.Pace.Current += e.Amount
			}
			if date >= lastMondayStr && date <= lastPaceEnd {
				week.Pace.Previous += e.Amount
			}
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

// at returns the slice element at i, or nil when i is out of range — the
// trailing window is positional, so a short slice is a hole, not an error.
func at[T any](s []*T, i int) *T {
	if i < 0 || i >= len(s) {
		return nil
	}
	return s[i]
}
