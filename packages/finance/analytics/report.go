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

// DigestLines renders how the month stands for the daily WhatsApp digest: the
// traffic light and what is going wrong, over the days that have finished.
//
// The digest goes out in the morning, so every line here is explicitly about
// the past — never about today, which has not been traded yet. On the first day
// of a month there is no past at all, and saying so is the whole content: the
// figures that used to be printed there ("saúde crítica", "receita caiu 100%")
// were the month's booked bills weighed against sales nobody had made yet.
// What to do from here is a separate half of the message, built from
// AheadLines.
//
// Only warnings and criticals make the cut — "resultado positivo" is good news
// but it is not news, and the digest competes with everything else in the
// user's WhatsApp.
func (a Analysis) DigestLines() []string {
	if a.Period.InProgress && a.Period.ThroughDay == 0 {
		return []string{"O mês está começando — ainda não há dia fechado para comparar."}
	}

	header := fmt.Sprintf("Saúde do mês: %s.", a.Health.Status.Label())
	if a.Period.InProgress {
		header = fmt.Sprintf("Saúde do mês até ontem (dia %d): %s.", a.Period.ThroughDay, a.Health.Status.Label())
	}
	lines := []string{header}

	shown := 0
	for _, m := range a.Health.Messages {
		// The goal insights are about the days still to come, and AheadLines
		// already prices them. Repeating them here put the same "necessário
		// R$ X/dia" under a heading that says the opposite.
		if m.Severity == SeverityInfo || m.Type == InsightGoalBehind || m.Type == InsightGoalOnTrack {
			continue
		}
		if shown >= maxDigestInsights {
			break
		}
		lines = append(lines, fmt.Sprintf("%s — %s.", m.Title, m.Description))
		shown++
	}

	// Said once, plainly, rather than left as a silence: through the 7th there
	// is no month-over-month line in the digest at all, and a reader who saw one
	// yesterday is owed the reason. Last, because it explains an absence —
	// whatever is actually wrong with the month comes first.
	if a.Period.InProgress && a.Period.ComparableThroughDay == 0 {
		lines = append(lines, fmt.Sprintf(
			"Comparação com o mês passado a partir do dia %d — a primeira semana ainda não fechou.", daysInWeek+1,
		))
	}

	return lines
}

// AheadLines renders what the days still to come have to bring — the half of
// the digest that is actionable at the hour it arrives, and the only half that
// says anything at all on the first day of a month.
//
// The per-day ask comes first because it is the number to act on; a
// recommendation follows as the reason. A closed month has nothing ahead of it
// and yields no lines.
func (a Analysis) AheadLines() []string {
	if !a.Period.InProgress {
		return nil
	}

	var lines []string
	if a.Projection.Pacing() && a.Projection.NeededPerDay > 0 {
		lines = append(lines, fmt.Sprintf("Faltam %s para a meta: %s/dia nos %s que restam (hoje incluído).",
			formatBRL(a.Projection.Target-a.Projection.Actual),
			formatBRL(a.Projection.NeededPerDay),
			pluralDias(a.Projection.DaysRemaining)))
	}
	// From the top. This used to skip recommendations[0] whenever a per-day ask
	// had been printed, because the weekly-pace one repeated it word for word —
	// and kept skipping it once the projection verdict took its place.
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
		// faturamento is what was sold; entradas_de_caixa is everything that
		// arrived, loans and aportes included. They are different numbers on
		// purpose and the model must not present either as the other — the
		// system prompt spells that out.
		"faturamento":       reais(a.KPIs.Faturamento),
		"entradas_de_caixa": reais(a.KPIs.EntradasCaixa),
		// despesa is what has actually left the account so far; despesa_agendada
		// is what is booked for the days still to come. Kept apart because the
		// model was handed their sum and reported the month's spending as
		// already done — on the 3rd, every bill of August.
		"despesa":          reais(a.KPIs.Despesa),
		"despesa_agendada": reais(a.KPIs.DespesaAgendada),
		"resultado":        reais(a.KPIs.Resultado),
		// Today counts as a day still to sell on, so the model never tells
		// someone on the last day of the month that there is nothing left to do.
		"dias_restantes_no_mes_com_hoje": a.Period.DaysRemaining,
		// Every percentage below is measured over both months' days up to this
		// one — whole weeks from the 1st, so the two sides hold the same days of
		// the week. The model must not present it as a whole month, and the
		// system prompt says so — see apps/notifier. Zero means there is no
		// comparison to quote; the flags below spell out which case it is.
		"comparacao_ate_o_dia": a.Period.ComparableThroughDay,
		// The month's own days, which run further than the comparison window
		// during the first week of a month. The result and the traffic light are
		// over these.
		"mes_fechado_ate_o_dia": a.Period.ThroughDay,
		"tendencia": map[string]any{
			"faturamento_pct": a.Trends.Faturamento.Change,
			"despesa_pct":     a.Trends.Despesa.Change,
			"resultado_pct":   a.Trends.Resultado.Change,
		},
		"semana": map[string]any{
			"faturamento_atual":          reais(a.WeekComparison.Current),
			"faturamento_semana_passada": reais(a.WeekComparison.Previous),
			// Both sides cover the same finished days of the week; today is in
			// neither.
			"ritmo_ate_ontem":               reais(a.WeekComparison.Pace.Current),
			"ritmo_semana_passada_ate_aqui": reais(a.WeekComparison.Pace.Previous),
		},
		// The same projection the dashboard draws. It used to be derived here
		// from last week's flat daily rate while the page used the weekday
		// averages, so the bot and the screen quoted different figures for the
		// same month.
		"projecao_do_mes": reais(a.Projection.Projected),
		// How the projection was arrived at, and how much trading it stands on.
		// Spelled out because the model reads the figure aloud, and a projection
		// built on a user's first three days must not be quoted with the same
		// confidence as one built on eight weeks.
		"projecao_base": string(a.Projection.Basis),
		// The goal is a sales target, so it is measured against faturamento —
		// see domain.IsRevenue. It can differ from "faturamento" above only in
		// that the goal is capped at 100%.
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
		"recomendacoes":           recommendationTexts(a.Recommendations),
		"maiores_despesas":        topExpenses(a.ExpenseComposition),
		"melhor_dia":              dayText(a.Highlights.BestIncome),
		"pior_dia":                dayText(a.Highlights.WorstIncome),
		"media_por_dia_da_semana": weekdayToolPayload(a.Weekdays),
	}

	// Spelled out rather than left for the model to infer from
	// comparacao_ate_o_dia being 0, which it does not reliably do: on the first
	// day of a month it read the empty month-over-month figures as a real
	// collapse and reported a 100% fall in receita.
	if a.Period.InProgress && a.Period.ThroughDay == 0 {
		payload["mes_comecando_sem_dia_fechado"] = true
	}
	// The same spelling-out for the rest of the opening week, where the month
	// *has* closed days to report on but none that can be held against the
	// previous month: the 1st and 2nd of a month are not the same weekdays as
	// the 1st and 2nd of the one before, and the model presented the resulting
	// percentage as a real fall.
	if a.Period.InProgress && a.Period.ComparableThroughDay == 0 && a.Period.ThroughDay > 0 {
		payload["sem_semana_fechada_para_comparar"] = true
	}
	// Only where a window was actually consulted. A closed month projected
	// nothing and an empty window measured nothing, and quoting "janela de 8
	// semanas" beside either invites the model to describe a figure as an
	// eight-week estimate when no window produced it.
	if a.Projection.Basis == ProjectionFromWindow || a.Projection.Basis == ProjectionPartial {
		payload["projecao_janela_em_semanas"] = projectionWindowWeeks
	}
	// Whether the runway below already counts an ordinary day's receipts. False
	// means it does not — there is no trading history — and the model must not
	// read the figures as a balance about to run out.
	payload["caixa"].(map[string]any)["conta_recebimento_esperado"] = a.CashPosition.ExpectsReceipts
	if a.Projection.NeededPerDay > 0 {
		payload["necessario_por_dia_para_bater_a_meta"] = reais(a.Projection.NeededPerDay)
	}
	if a.Projection.Gap > 0 {
		payload["falta_para_a_meta_na_projecao"] = reais(a.Projection.Gap)
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

// weekdayToolPayload renders the per-weekday averages for the AI bot. The model
// reads these numbers aloud, so they are in reais and carry enough context
// (semanas, base) for the bot to qualify the figure rather than stating it as
// an absolute truth.
func weekdayToolPayload(days []WeekdayStat) []map[string]any {
	out := make([]map[string]any, 0, len(days))
	for _, d := range days {
		out = append(out, map[string]any{
			"dia":     weekdayFullLabels[d.Day],
			"media":   reais(d.Avg),
			"semanas": d.Count,
			"base":    string(d.Basis),
		})
	}
	return out
}
