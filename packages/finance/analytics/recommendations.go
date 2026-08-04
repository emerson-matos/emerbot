package analytics

import "fmt"

// Thresholds above which a month-over-month move is worth a recommendation of
// its own, on top of whatever the health insights already said.
const (
	expenseSpikePct = 15
	incomeSlumpPct  = 10
	// cashRunwayDays — a balance going negative this soon needs acting on now,
	// not at month end.
	cashRunwayDays = 7
)

// buildRecommendations turns the month's state into actionable next steps.
//
// The list is read whole. recommendations[0] used to be the weekly-pace one,
// which repeated the per-day ask its consumers had just printed, so both the
// dashboard and the digest lifted it out and rendered the rest — a coupling
// that survived the message changing underneath it and silently swallowed the
// projection verdict that replaced it. Nothing here is positional now.
//
// window is the comparison the trends were measured over; its suffix is
// repeated in every month-over-month message, because a percentage from half a
// month presented as a whole one is how "receita caiu 100%" reached a WhatsApp.
func buildRecommendations(week WeekComparison, projection Projection, trends Trends, cash CashPosition, window comparison) []Recommendation {
	recs := []Recommendation{}

	if projection.Pacing() {
		recs = append(recs, projectionRecommendation(projection))
	}

	if hasBaseline(trends.Despesa) && trends.Despesa.Direction == TrendUp && trends.Despesa.Change > expenseSpikePct {
		recs = append(recs, Recommendation{
			Severity: RecWarning,
			Title:    "Despesas acima do normal",
			Message: fmt.Sprintf("Cresceram %d%% vs mês passado%s. Revise gastos para manter a margem.",
				trends.Despesa.Change, window.windowSuffix()),
		})
	}

	if hasBaseline(trends.Faturamento) && trends.Faturamento.Direction == TrendDown && abs(trends.Faturamento.Change) > incomeSlumpPct {
		recs = append(recs, Recommendation{
			Severity: RecDanger,
			Title:    "Receita caiu",
			Message: fmt.Sprintf("%d%% abaixo do mês passado%s. Identifique causas e aja rapidamente.",
				abs(trends.Faturamento.Change), window.windowSuffix()),
		})
	}

	// ExpectsReceipts gates this the way hasBaseline gates the two above, and for
	// the same reason: without any trading history the days ahead are priced at
	// nothing, so the balance crosses zero on paper for a pharmacy that has
	// simply not recorded a sale yet. "Reduza despesas" is not the advice that
	// case needs.
	if cash.ExpectsReceipts && cash.DaysUntilNegative != nil && *cash.DaysUntilNegative <= cashRunwayDays {
		recs = append(recs, Recommendation{
			Severity: RecDanger,
			Title:    "Saldo fica negativo em breve",
			Message: fmt.Sprintf("Mesmo contando o recebimento de um dia normal, o saldo fica negativo em %s. Reduza despesas ou antecipe recebimentos.",
				pluralDias(*cash.DaysUntilNegative)),
		})
	}

	return recs
}

// hasBaseline reports whether a trend has a previous month to be a percentage
// of. buildTrend reports a previous of zero as a flat 100% rise (there is no
// meaningful percentage over nothing), so without this a month whose
// predecessor never traded would be told its expenses "cresceram 100% vs mês
// passado" against a month that saw no expenses at all. The health insights
// already require a real baseline; these say the same thing to the same person
// on the same page, so they require one too.
//
// It also covers the month's opening week, where buildTrends zeroes both sides
// because there is no like-for-like window yet — that is what used to open the
// 1st with "Receita caiu 100% abaixo do mês passado", and the 3rd with a red
// alert for a pharmacy trading exactly as it had the month before.
func hasBaseline(t MonthTrend) bool { return t.Previous > 0 }

// projectionRecommendation interprets the projection as a verdict on the
// month. It reads the Status the projection already carries rather than
// dividing again for itself, so the title here cannot disagree with the colour
// the card draws from the same field.
func projectionRecommendation(projection Projection) Recommendation {
	var severity RecommendationSeverity
	var title string
	switch projection.Status {
	case ProjNoTarget:
		return Recommendation{}
	case ProjSuccess:
		severity, title = RecSuccess, "Ritmo suficiente"
	case ProjWarning:
		severity, title = RecWarning, "Projeção abaixo da meta"
	default:
		severity, title = RecDanger, "Projeção muito abaixo da meta"
	}

	return Recommendation{Severity: severity, Title: title, Message: projectionMessage(projection)}
}

// projectionMessage words the verdict. It switches on Status rather than on
// coverage so the sentence cannot end up graded differently from the title
// above it; the one extra cut, inside ProjDanger, separates a goal that needs a
// hard acceleration from one that is out of reach, and reads the same const
// every other threshold here does.
func projectionMessage(projection Projection) string {
	switch projection.Status {
	case ProjSuccess:
		return "O ritmo atual deve fechar o mês muito próximo da meta. Manter o desempenho nas próximas semanas deve ser suficiente para alcançá-la."
	case ProjWarning:
		return fmt.Sprintf("O ritmo atual deve fechar o mês em torno de %s. Será necessário aumentar as vendas nas próximas semanas para alcançar %s.",
			formatBRL(projection.Projected), formatBRL(projection.Target))
	default:
		if projection.Coverage >= coverageOutOfReach {
			return fmt.Sprintf("No ritmo atual o mês deve fechar em torno de %s. Será preciso uma aceleração consistente para alcançar %s.",
				formatBRL(projection.Projected), formatBRL(projection.Target))
		}
		return fmt.Sprintf("No ritmo atual o mês deve fechar em torno de %s. A meta de %s exige um desempenho muito acima do histórico recente.",
			formatBRL(projection.Projected), formatBRL(projection.Target))
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
