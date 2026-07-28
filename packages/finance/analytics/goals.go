package analytics

import (
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// goalProgress measures the month against its targets. faturamento is passed
// in rather than read off the summary's TotalIncome — despite the field being
// called IncomeActual, it is faturamento (venda_balcao + convenio + delivery,
// see isFaturamento), not the summary's broader total, because the goal is a
// sales target: a loan or aporte filed under outros_receitas must not count
// toward it. Sharing one pass over the entries with everything else in Build
// that measures faturamento keeps the two from disagreeing about the same
// month.
//
// Percentages are capped at 100 so a bar cannot overflow its track; the raw
// amounts stay uncapped for anyone who wants the overshoot.
func goalProgress(summary pkgfinance.MonthlySummary, goal *domain.Goal, now time.Time, faturamento int64) GoalProgress {
	daysTotal := daysInMonth(now)
	progress := GoalProgress{
		IncomeActual:  faturamento,
		ExpenseActual: summary.TotalExpense,
		DaysRemaining: daysTotal - now.Day(),
		DaysTotal:     daysTotal,
	}
	if goal == nil {
		return progress
	}

	progress.IncomeTarget = goal.IncomeTarget
	progress.ExpenseTarget = goal.ExpenseTarget
	if goal.IncomeTarget > 0 {
		progress.IncomePct = min(100, roundToInt(float64(faturamento)/float64(goal.IncomeTarget)*100))
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
// snapshot.Income is the stored summary's TotalIncome — receita, not
// faturamento (see isFaturamento) — because the summaries this reads are
// pre-aggregated per month and were never split by category. A past month
// with an outros_receitas entry will show a slightly higher Income here than
// its IncomeTarget was actually tracked against; only the *current* month's
// GoalProgress.IncomeActual (goalProgress, above) is guaranteed faturamento.
func buildHistory(months []string, summaries []*pkgfinance.MonthlySummary, goals []*domain.Goal) []MonthlySnapshot {
	out := make([]MonthlySnapshot, 0, len(months))
	for i, month := range months {
		snapshot := MonthlySnapshot{Month: month, Label: month}
		if t, _, err := domain.ParseMonth(month); err == nil {
			snapshot.Label = monthYearLabel(t)
		}
		if i < len(summaries) && summaries[i] != nil {
			snapshot.Income = summaries[i].TotalIncome
			snapshot.Expense = summaries[i].TotalExpense
		}
		if i < len(goals) && goals[i] != nil {
			income, expense := goals[i].IncomeTarget, goals[i].ExpenseTarget
			snapshot.IncomeTarget = &income
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
