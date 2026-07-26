package analytics

import (
	"fmt"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// Thresholds for the insight rules. Named because the numbers are a product
// decision, not an implementation detail, and they are read in two places
// (health and recommendations) that must agree.
const (
	// expenseGrowthPct — expenses growing faster than this, and faster than
	// income, is worth flagging.
	expenseGrowthPct = 10
	// revenueDropPct — income falling more than this against last month.
	revenueDropPct = -10
	// weekPacePct — the dead band for week-over-week pace, either direction.
	weekPacePct = 5
	// onTrackMargin — the current daily rate has to clear what is still needed
	// per day by this much to count as "on track", so a rate sitting exactly on
	// the requirement is not called safe.
	onTrackMargin = 1.05
)

// buildHealth turns the month's numbers into the traffic light and the list of
// insights behind it.
// The month-over-month insights read `compared`, not the raw summaries: those
// hold a month in progress against a month that finished, which reads as a
// collapse in revenue every time the month is young. Everything else here is
// about the month on its own and uses the full summary.
func buildHealth(
	entries []domain.FinancialEntry,
	summary pkgfinance.MonthlySummary,
	compared comparison,
	week WeekComparison,
	goals GoalProgress,
) Health {
	messages := []Insight{}

	positiveDays, totalDays := countDays(entries)

	if summary.Balance > 0 {
		messages = append(messages, Insight{
			Type:        InsightGoodPerformance,
			Severity:    SeverityInfo,
			Title:       "Resultado positivo",
			Description: "Receitas maiores que despesas",
		})
	}

	if totalDays > 0 {
		messages = append(messages, Insight{
			Type:        InsightGoodPerformance,
			Severity:    SeverityInfo,
			Title:       fmt.Sprintf("%d dos %d dias", positiveDays, totalDays),
			Description: "Dias fecharam no azul",
		})
	}

	if summary.TotalIncome > 0 {
		pct := roundToInt(float64(summary.TotalExpense) / float64(summary.TotalIncome) * 100)
		messages = append(messages, Insight{
			Type:        InsightGoodPerformance,
			Severity:    SeverityInfo,
			Title:       fmt.Sprintf("Despesas representam %d%%", pct),
			Description: "das receitas",
		})
	}

	// Month-over-month only says anything when both months actually traded up
	// to this point; a percentage against a month with no income is noise.
	if compared.previous.income > 0 && compared.current.income > 0 {
		incomeChange := percentChange(compared.current.income, compared.previous.income)
		var expenseChange float64
		if compared.previous.expense > 0 {
			expenseChange = percentChange(compared.current.expense, compared.previous.expense)
		}

		// Expenses growing is only a problem when income is not keeping up.
		if expenseChange > expenseGrowthPct && incomeChange < expenseChange {
			messages = append(messages, Insight{
				Type:        InsightExpenseGrowth,
				Severity:    SeverityWarning,
				Title:       "Despesas cresceram",
				Description: fmt.Sprintf("%d%% acima do mês passado%s", roundToInt(expenseChange), compared.suffix()),
				Value:       ptr(expenseChange),
			})
		}

		if incomeChange < revenueDropPct {
			messages = append(messages, Insight{
				Type:        InsightRevenueDrop,
				Severity:    SeverityWarning,
				Title:       "Receitas cairam",
				Description: fmt.Sprintf("%d%% abaixo do mês passado%s", roundToInt(-incomeChange), compared.suffix()),
				Value:       ptr(incomeChange),
			})
		}
	}

	if summary.Balance < 0 {
		messages = append(messages, Insight{
			Type:        InsightLowCashFlow,
			Severity:    SeverityCritical,
			Title:       "Fluxo negativo",
			Description: "Resultado negativo no mês",
		})
	}

	if week.PreviousUpToDay != 0 {
		weekPct := percentChange(week.Current, week.PreviousUpToDay)
		switch {
		case weekPct > weekPacePct:
			messages = append(messages, Insight{
				Type:        InsightWeeklyImprovement,
				Severity:    SeverityInfo,
				Title:       "Ritmo subiu vs semana passada",
				Description: fmt.Sprintf("%d%% acima", roundToInt(weekPct)),
				Value:       ptr(weekPct),
			})
		case weekPct < -weekPacePct:
			messages = append(messages, Insight{
				Type:        InsightWeeklyDecline,
				Severity:    SeverityWarning,
				Title:       "Ritmo caiu vs semana passada",
				Description: fmt.Sprintf("%d%% abaixo", roundToInt(-weekPct)),
				Value:       ptr(weekPct),
			})
		}
	}

	if pace, ok := goalPace(goals); ok {
		if pace.onTrack {
			description := "Faturamento já superou a meta"
			if pace.neededPerDay > 0 {
				description = fmt.Sprintf("Necessário %s/dia — você está acima", formatBRLFloat(pace.neededPerDay))
			}
			messages = append(messages, Insight{
				Type:        InsightGoalOnTrack,
				Severity:    SeverityInfo,
				Title:       "No ritmo para bater a meta",
				Description: description,
			})
		} else if pace.neededPerDay > 0 {
			messages = append(messages, Insight{
				Type:     InsightGoalBehind,
				Severity: SeverityWarning,
				Title:    "Precisa acelerar para bater a meta",
				Description: fmt.Sprintf("Necessário %s/dia nos próximos %s",
					formatBRLFloat(pace.neededPerDay), pluralDias(goals.DaysRemaining)),
				Value: ptr(pace.neededPerDay),
			})
		}
	}

	return Health{Status: healthStatus(summary.Balance, messages), Messages: messages}
}

// pace is the goal arithmetic shared by the goal insight and the weekly
// recommendation: what the month still has to average per day, and whether
// the rate so far clears it.
type pace struct {
	neededPerDay float64
	onTrack      bool
}

// goalPace reports false when there is nothing to pace against — no target, no
// days left, or a month that has not started.
func goalPace(goals GoalProgress) (pace, bool) {
	if goals.RevenueTarget <= 0 || goals.DaysRemaining <= 0 || goals.DaysTotal <= 0 {
		return pace{}, false
	}
	elapsed := goals.DaysTotal - goals.DaysRemaining
	if elapsed <= 0 {
		return pace{}, false
	}
	currentDailyRate := float64(goals.RevenueActual) / float64(elapsed)
	neededPerDay := float64(goals.RevenueTarget-goals.RevenueActual) / float64(goals.DaysRemaining)
	return pace{
		neededPerDay: neededPerDay,
		onTrack:      currentDailyRate >= neededPerDay*onTrackMargin,
	}, true
}

// countDays returns how many days closed in the black, and how many days saw
// any entry at all. A day is in the black when its counter sales beat
// everything else that moved that day.
func countDays(entries []domain.FinancialEntry) (positive, total int) {
	byDate := map[string]int64{}
	for _, e := range entries {
		date := e.TransactionDate.String()
		if isCounterSale(e) {
			byDate[date] += e.Amount
		} else {
			byDate[date] -= e.Amount
		}
	}
	for _, balance := range byDate {
		if balance > 0 {
			positive++
		}
	}
	return positive, len(byDate)
}

// healthStatus collapses the insights into one traffic light. A negative
// balance is critical on its own, regardless of how many cheerful insights
// came before it.
func healthStatus(balance int64, messages []Insight) HealthStatus {
	if balance < 0 {
		return HealthCritico
	}
	status := HealthBoa
	for _, m := range messages {
		switch m.Severity {
		case SeverityCritical:
			return HealthCritico
		case SeverityWarning:
			status = HealthAtencao
		}
	}
	return status
}

// percentChange is the change from previous to current, as a percentage of
// previous. Callers must ensure previous is non-zero.
func percentChange[T int64 | int](current, previous T) float64 {
	return float64(current-previous) / float64(previous) * 100
}

func ptr[T any](v T) *T { return &v }
