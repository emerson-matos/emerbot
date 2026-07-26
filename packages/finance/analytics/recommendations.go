package analytics

import "fmt"

// Thresholds above which a month-over-month move is worth a recommendation of
// its own, on top of whatever the health insights already said.
const (
	expenseSpikePct = 15
	revenueSlumpPct = 10
	// cashRunwayDays — a balance going negative this soon needs acting on now,
	// not at month end.
	cashRunwayDays = 7
)

// buildRecommendations turns the month's state into actionable next steps.
//
// The first recommendation is always the weekly one when a goal exists: the
// dashboard pulls recommendations[0] out to caption the week-comparison card,
// and the notifier leads its digest with it.
func buildRecommendations(week WeekComparison, goals GoalProgress, trends Trends, cash CashPosition) []Recommendation {
	recs := []Recommendation{}

	if p, ok := goalPace(goals); ok {
		recs = append(recs, weeklyPaceRecommendation(week, goals, p))
	}

	if trends.Despesa.Direction == TrendUp && trends.Despesa.Change > expenseSpikePct {
		recs = append(recs, Recommendation{
			Severity: RecWarning,
			Title:    "Despesas acima do normal",
			Message: fmt.Sprintf("Cresceram %d%% vs mês passado. Revise gastos para manter a margem.",
				trends.Despesa.Change),
		})
	}

	if trends.Receita.Direction == TrendDown && abs(trends.Receita.Change) > revenueSlumpPct {
		recs = append(recs, Recommendation{
			Severity: RecDanger,
			Title:    "Receita caiu",
			Message: fmt.Sprintf("%d%% abaixo do mês passado. Identifique causas e aja rapidamente.",
				abs(trends.Receita.Change)),
		})
	}

	if cash.DaysUntilNegative != nil && *cash.DaysUntilNegative <= cashRunwayDays {
		recs = append(recs, Recommendation{
			Severity: RecDanger,
			Title:    "Saldo fica negativo em breve",
			Message: fmt.Sprintf("O saldo fica negativo em %s. Reduza despesas ou antecipe recebimentos.",
				pluralDias(*cash.DaysUntilNegative)),
		})
	}

	return recs
}

// weeklyPaceRecommendation crosses this week's pace with whether the month is
// on track for its goal. Both halves matter on their own — a week that
// improved but still misses the target needs a different message from one that
// improved and closes it — so all four combinations get their own copy rather
// than collapsing into "good" and "bad".
func weeklyPaceRecommendation(week WeekComparison, goals GoalProgress, p pace) Recommendation {
	var weekPct float64
	if week.PreviousUpToDay != 0 {
		weekPct = percentChange(week.Current, week.PreviousUpToDay)
	}
	improved := weekPct > weekPacePct
	behind := weekPct < -weekPacePct

	// The per-day ask, or an acknowledgement when the goal is already beaten
	// (neededPerDay goes non-positive once actual passes the target).
	needed := func(ifNeeded, ifBeaten string) string {
		if p.neededPerDay > 0 {
			return fmt.Sprintf(ifNeeded, formatBRLFloat(p.neededPerDay), pluralDias(goals.DaysRemaining))
		}
		return ifBeaten
	}

	switch {
	case improved && p.onTrack:
		return Recommendation{
			Severity: RecSuccess,
			Title:    "Ritmo subiu e fecha a meta",
			Message:  "O faturamento desta semana está acima da anterior. Mantenha esse ritmo para sustentar a projeção do mês.",
		}
	case improved:
		return Recommendation{
			Severity: RecWarning,
			Title:    "Ritmo subiu mas ainda falta",
			Message: needed(
				"Melhorou vs semana passada, mas precisa de %s/dia nos próximos %s para bater a meta.",
				"Melhorou vs semana passada. Continue nesse ritmo.",
			),
		}
	case behind && p.onTrack:
		return Recommendation{
			Severity: RecWarning,
			Title:    "Caiu mas a projeção fecha",
			Message:  "O faturamento desta semana está abaixo da anterior, mas a projeção do mês ainda está dentro da meta. Recupere o ritmo.",
		}
	case behind:
		return Recommendation{
			Severity: RecDanger,
			Title:    "Faturamento caiu e não bate a meta",
			Message: needed(
				"Precisa de %s/dia nos próximos %s para atingir a meta do mês.",
				"Faturamento caiu mas já superou a meta do mês.",
			),
		}
	case p.onTrack:
		return Recommendation{
			Severity: RecSuccess,
			Title:    "Ritmo estável e dentro da projeção",
			Message:  "O desempenho está consistente com a semana passada. Mantenha para preservar a projeção do mês.",
		}
	default:
		return Recommendation{
			Severity: RecWarning,
			Title:    "Ritmo estável mas não é suficiente",
			Message: needed(
				"Precisa acelerar para %s/dia nos próximos %s para bater a meta.",
				"Ritmo estável. Continue para preservar a projeção.",
			),
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
