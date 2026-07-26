package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// goalProgress measures the month against its targets. counterSales rather
// than the summary's TotalIncome is the revenue figure, because the target is
// set for what the counter takes — convênio and delivery receipts would
// otherwise flatter the number.
//
// Percentages are capped at 100 so a bar cannot overflow its track; the raw
// amounts stay uncapped for anyone who wants the overshoot.
func goalProgress(summary pkgfinance.MonthlySummary, goal *domain.Goal, now time.Time, counterSales int64) GoalProgress {
	daysTotal := daysInMonth(now)
	progress := GoalProgress{
		RevenueActual: counterSales,
		ExpenseActual: summary.TotalExpense,
		DaysRemaining: daysTotal - now.Day(),
		DaysTotal:     daysTotal,
	}
	if goal == nil {
		return progress
	}

	progress.RevenueTarget = goal.RevenueTarget
	progress.ExpenseTarget = goal.ExpenseTarget
	if goal.RevenueTarget > 0 {
		progress.RevenuePct = min(100, roundToInt(float64(counterSales)/float64(goal.RevenueTarget)*100))
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
func buildHistory(months []string, summaries []*pkgfinance.MonthlySummary, goals []*domain.Goal) []MonthlySnapshot {
	out := make([]MonthlySnapshot, 0, len(months))
	for i, month := range months {
		snapshot := MonthlySnapshot{Month: month, Label: month}
		if t, err := time.Parse("2006-01", month); err == nil {
			snapshot.Label = monthYearLabel(t)
		}
		if i < len(summaries) && summaries[i] != nil {
			snapshot.Income = summaries[i].TotalIncome
			snapshot.Expense = summaries[i].TotalExpense
		}
		if i < len(goals) && goals[i] != nil {
			revenue, expense := goals[i].RevenueTarget, goals[i].ExpenseTarget
			snapshot.IncomeTarget = &revenue
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
