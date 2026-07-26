package analytics

import (
	"fmt"
	"strings"
)

// maxDigestInsights caps how many warnings the WhatsApp digest repeats. The
// digest is a nudge, not a report — past a few lines people stop reading it,
// and the dashboard link is right underneath.
const maxDigestInsights = 3

// Label renders the status the way it is written to a person.
func (s HealthStatus) Label() string {
	switch s {
	case HealthBoa:
		return "Boa"
	case HealthAtencao:
		return "Atenção"
	case HealthCritico:
		return "Crítico"
	default:
		return string(s)
	}
}

// DigestLines renders the analysis as a few short Portuguese sentences for the
// daily WhatsApp digest: the traffic light, what is going wrong, and the one
// thing to do about it.
//
// Only warnings and criticals make the cut — "resultado positivo" is good news
// but it is not news, and the digest competes with everything else in the
// user's WhatsApp.
func (a Analysis) DigestLines() []string {
	lines := []string{fmt.Sprintf("Saúde do mês: %s.", a.Health.Status.Label())}

	shown := 0
	for _, m := range a.Health.Messages {
		if m.Severity == SeverityInfo || shown >= maxDigestInsights {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s — %s.", m.Title, m.Description))
		shown++
	}

	if len(a.Recommendations) > 0 {
		r := a.Recommendations[0]
		lines = append(lines, fmt.Sprintf("%s: %s", r.Title, r.Message))
	}

	return lines
}

// ToolPayload renders the analysis for the AI bot. Amounts are in reais rather
// than centavos, because the model reads these numbers back to the user and
// would otherwise have to divide by 100 in prose — which it gets wrong.
//
// It is deliberately narrower than the full Analysis: the chart-shaped parts
// (per-weekday buckets, the daily balance curve, the history bars) are a lot of
// tokens for something the model cannot say out loud anyway.
func (a Analysis) ToolPayload() map[string]any {
	payload := map[string]any{
		"month": a.Month,
		"health": map[string]any{
			"status":   string(a.Health.Status),
			"messages": insightMessages(a.Health.Messages),
		},
		"receita":               reais(a.KPIs.Receita),
		"despesa":               reais(a.KPIs.Despesa),
		"resultado":             reais(a.KPIs.Resultado),
		"dias_restantes_no_mes": a.KPIs.DaysRemaining,
		"tendencia": map[string]any{
			"receita_pct":   a.Trends.Receita.Change,
			"despesa_pct":   a.Trends.Despesa.Change,
			"resultado_pct": a.Trends.Resultado.Change,
		},
		"semana": map[string]any{
			"faturamento_atual":           reais(a.WeekComparison.Current),
			"faturamento_semana_passada":  reais(a.WeekComparison.Previous),
			"mesmo_dia_da_semana_passada": reais(a.WeekComparison.PreviousUpToDay),
			"projecao_do_mes":             reais(a.WeekComparison.ProjectedMonthly),
		},
		"meta": map[string]any{
			"faturamento_meta":  reais(a.Goals.RevenueTarget),
			"faturamento_atual": reais(a.Goals.RevenueActual),
			"faturamento_pct":   a.Goals.RevenuePct,
			"despesa_teto":      reais(a.Goals.ExpenseTarget),
			"despesa_atual":     reais(a.Goals.ExpenseActual),
			"despesa_pct":       a.Goals.ExpensePct,
		},
		"caixa": map[string]any{
			"saldo_hoje":            reais(a.CashPosition.CurrentBalance),
			"projecao_fim_do_mes":   reais(a.CashPosition.EndOfMonthProjection),
			"menor_saldo_projetado": reais(a.CashPosition.LowestProjected),
			"menor_saldo_data":      a.CashPosition.LowestProjectedDate,
			// Absent rather than null when the balance never goes negative, so
			// the model has nothing to misread as "zero days left".
			"dias_ate_saldo_negativo": nil,
		},
		"recomendacoes":    recommendationTexts(a.Recommendations),
		"maiores_despesas": topExpenses(a.ExpenseComposition),
		"melhor_dia":       dayText(a.Highlights.BestIncome),
		"pior_dia":         dayText(a.Highlights.WorstIncome),
	}

	if p, ok := goalPace(a.Goals); ok && p.neededPerDay > 0 {
		payload["necessario_por_dia_para_bater_a_meta"] = reais(roundToInt64(p.neededPerDay))
	}
	if d := a.CashPosition.DaysUntilNegative; d != nil {
		payload["caixa"].(map[string]any)["dias_ate_saldo_negativo"] = *d
	}

	return payload
}

func insightMessages(insights []Insight) []string {
	out := make([]string, 0, len(insights))
	for _, m := range insights {
		out = append(out, fmt.Sprintf("%s — %s", m.Title, m.Description))
	}
	return out
}

func recommendationTexts(recs []Recommendation) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, fmt.Sprintf("[%s] %s: %s", r.Severity, r.Title, r.Message))
	}
	return out
}

// topExpenses keeps the composition short enough to be quoted in a reply.
func topExpenses(composition []ExpenseComposition) []map[string]any {
	const maxCategories = 5
	if len(composition) > maxCategories {
		composition = composition[:maxCategories]
	}
	out := make([]map[string]any, 0, len(composition))
	for _, c := range composition {
		out = append(out, map[string]any{
			"categoria": c.CategoryName,
			"valor":     reais(c.Amount),
			"pct":       c.Percentage,
		})
	}
	return out
}

func dayText(h DayHighlight) string {
	if h.Date == NoDataDate {
		return h.Label
	}
	return strings.TrimSpace(fmt.Sprintf("%s (%s)", h.Label, formatBRL(h.Amount)))
}

// reais converts centavos for the model's benefit — see ToolPayload.
func reais(centavos int64) float64 {
	return float64(centavos) / 100
}
