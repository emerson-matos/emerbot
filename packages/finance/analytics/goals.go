package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// goalProgress measures the month against its targets. The revenue figure is
// passed in rather than re-derived so that every faturamento number in one
// Analysis comes from the same place and they cannot disagree about the same
// month.
//
// Percentages are capped at 100 so a bar cannot overflow its track; the raw
// amounts stay uncapped for anyone who wants the overshoot.
func goalProgress(summary pkgfinance.MonthlySummary, goal *domain.Goal, clock monthClock, revenue int64) GoalProgress {
	progress := GoalProgress{
		RevenueActual: revenue,
		ExpenseActual: summary.TotalExpense,
		// Today counts as a day still to trade, and a closed month has none —
		// the day numbers used to be read off now's calendar whatever month was
		// being analysed, so a finished July opened in August still had "30
		// dias" to reach its target in.
		DaysRemaining: clock.remaining,
		DaysTotal:     clock.total,
	}
	if goal == nil {
		return progress
	}

	progress.RevenueTarget = goal.RevenueTarget
	progress.ExpenseTarget = goal.ExpenseTarget
	if goal.RevenueTarget > 0 {
		progress.RevenuePct = min(100, roundToInt(float64(revenue)/float64(goal.RevenueTarget)*100))
	}
	if goal.ExpenseTarget > 0 {
		progress.ExpensePct = min(100, roundToInt(float64(summary.TotalExpense)/float64(goal.ExpenseTarget)*100))
	}
	return progress
}

// buildHistory renders the trailing three-month window as chart-ready
// snapshots. summaries and goals stay positionally aligned with months — a
// month with no data is a nil hole, never a dropped slot, since collapsing it
// would shift every other month onto the wrong label.
//
// snapshot.Revenue is faturamento, the same figure RevenueTarget is tracked
// against, so a bar and its target line finally measure the same thing. They
// used to disagree: the summaries were not split by origin, so this read the
// broad income total against a sales target — a past month with a loan in it
// looked closer to its goal than it was.
func buildHistory(months []string, summaries []*pkgfinance.MonthlySummary, goals []*domain.Goal) []MonthlySnapshot {
	out := make([]MonthlySnapshot, 0, len(months))
	for i, month := range months {
		snapshot := MonthlySnapshot{Month: month, Label: month}
		if t, _, err := domain.ParseMonth(month); err == nil {
			snapshot.Label = monthYearLabel(t)
		}
		if i < len(summaries) && summaries[i] != nil {
			snapshot.Revenue = summaries[i].TotalRevenue
			snapshot.Expense = summaries[i].TotalExpense
		}
		if i < len(goals) && goals[i] != nil {
			revenue, expense := goals[i].RevenueTarget, goals[i].ExpenseTarget
			snapshot.RevenueTarget = &revenue
			snapshot.ExpenseTarget = &expense
		}
		out = append(out, snapshot)
	}
	return out
}

// daysInMonth returns the number of days in the calendar month t falls in.
func daysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}
