package analytics

import (
	"fmt"
	"math"
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
// The day's ask comes first because it is the number to act on; a
// recommendation follows as the reason. A closed month has nothing ahead of it
// and yields no lines.
//
// The digest carried no figure of its own for a while, which left the model
// rewriting it to invent one — and the only arithmetic available to it was the
// gap over the days left, a flat daily ask that reads the same on a Sunday as
// on a Saturday. The ask is stated here, scaled by the weekday, so there is
// nothing left for it to derive.
func (a Analysis) AheadLines() []string {
	if !a.Period.InProgress {
		return nil
	}

	var lines []string
	if t := a.Projection.TodayTarget; t.Asked() {
		lines = append(lines, todayTargetLine(t))
	}
	if len(a.Recommendations) > 0 {
		r := a.Recommendations[0]
		lines = append(lines, fmt.Sprintf("%s: %s", r.Title, r.Message))
	}
	return lines
}

// todayTargetLine words the day's ask against the day's own history, because
// the ask means nothing without it: "R$ 1.480,00" on a Sunday is either a
// quiet morning or an impossible one depending on what Sundays bring, and the
// person reading it at seven in the morning is owed which.
func todayTargetLine(t DayTarget) string {
	day := weekdayNames[int(t.Day)]
	article := weekdayWithArticle(t.Day)
	pct := roundToInt(math.Abs(t.DeltaPercent) * 100)
	switch t.Status {
	case PaceAbove:
		return fmt.Sprintf("Meta de hoje (%s): %s — %d%% acima do que %s costuma faturar (%s).",
			day, formatBRL(t.Target), pct, article, formatBRL(t.Historical))
	case PaceBelow:
		return fmt.Sprintf("Meta de hoje (%s): %s — %d%% abaixo do que %s costuma faturar (%s), o mês está adiantado.",
			day, formatBRL(t.Target), pct, article, formatBRL(t.Historical))
	default:
		return fmt.Sprintf("Meta de hoje (%s): %s — em linha com o que %s costuma faturar (%s).",
			day, formatBRL(t.Target), article, formatBRL(t.Historical))
	}
}

// Section names a block of the analysis the base payload leaves out. Everything
// the dashboard can show, the model can reach — but not all of it in every
// answer: "como estamos?" is asked far more often than "quais foram os dias de
// maior saída?", and a payload carrying both costs the same tokens either way,
// on a project with a hard monthly cost cap.
//
// So the base payload advertises these (secoes_disponiveis) and the tool takes
// them as an argument. Availability without weight: the model asks for what the
// question needs, and nothing the page renders is unreachable from a chat.
type Section string

const (
	// SectionCashOutDays is the month's heaviest spending days, broken down by
	// category — the "onde o dinheiro saiu" list on the page.
	SectionCashOutDays Section = "dias_de_saida"
	// SectionExpensesFull is the whole expense composition. The base payload
	// carries the five largest categories and says so; this is the rest.
	SectionExpensesFull Section = "despesas_completas"
	// SectionHistory is the trailing three months with their targets — the bar
	// chart at the bottom of the page.
	SectionHistory Section = "historico"
	// SectionHighlights is best and worst day by balance. The base payload
	// already carries the best and worst by faturamento, which is what most
	// questions are about.
	SectionHighlights Section = "destaques"
)

// sectionCatalog is what the base payload advertises: the name to ask for and
// what arrives if you do. It is written once here so the tool's schema, the
// payload's own index and this file cannot drift into describing different sets.
var sectionCatalog = []struct {
	Name Section
	What string
}{
	{SectionCashOutDays, "dias de maior saída de caixa, com as categorias de cada um"},
	{SectionExpensesFull, "composição completa das despesas por categoria"},
	{SectionHistory, "os três meses anteriores com faturamento, despesa e metas"},
	{SectionHighlights, "melhor e pior dia por saldo (o payload já traz por faturamento)"},
}

// AllSections is every section, for a caller that genuinely wants the lot.
func AllSections() []Section {
	out := make([]Section, 0, len(sectionCatalog))
	for _, s := range sectionCatalog {
		out = append(out, s.Name)
	}
	return out
}

// maxToolCategories is how many expense categories the base payload quotes.
// Beyond it the list is cut — and says it was cut, with the section to ask for,
// because a partial ranking presented as the whole one is exactly what ADR-015
// exists to prevent.
const maxToolCategories = 5

// ToolPayload renders the analysis for the AI bot. Amounts are in reais rather
// than centavos, because the model reads these numbers back to the user and
// would otherwise have to divide by 100 in prose — which it gets wrong.
//
// The base payload answers the questions actually asked of a chat assistant —
// how is the month, will it hit the goal, what should I do, how much today. The
// heavier blocks are reachable through sections rather than always sent; see
// Section. Nothing the dashboard renders is out of the model's reach, and
// nothing it rarely needs is paid for on every turn.
func (a Analysis) ToolPayload(sections ...Section) map[string]any {
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
		// despesa is what has actually left the account so far. What is merely
		// booked for the rest of the month is not here at all: it is a
		// commitment, and it lives under caixa beside the runway that says
		// whether there is money for it — see compromissos_do_mes.
		"despesa":   reais(a.KPIs.Despesa),
		"resultado": reais(a.KPIs.Resultado),
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
			// The month's booked bills, and whether the balance coming in covers
			// them. They sit here rather than beside despesa because that is the
			// only question they raise: a commitment is about liquidity, never
			// about how the month is performing. Listed among the month's result
			// figures, the amount read as a finding on its own — "o volume de
			// despesas agendadas é um ponto de atenção", on a month whose runway
			// never went near zero.
			"compromissos_do_mes":   reais(a.KPIs.DespesaAgendada),
			"compromissos_situacao": string(a.CashPosition.Commitments),
			"saldo_hoje":            reais(a.CashPosition.CurrentBalance),
			"projecao_fim_do_mes":   reais(a.CashPosition.EndOfMonthProjection),
			"menor_saldo_projetado": reais(a.CashPosition.LowestProjected),
			"menor_saldo_data":      a.CashPosition.LowestProjectedDate,
			// dias_ate_saldo_negativo is deliberately not seeded here — see the
			// conditional near the end of this function. The key used to be
			// declared as an explicit nil, which is absent from a Go map lookup
			// but serializes to `"dias_ate_saldo_negativo": null`. The digest now
			// hands this payload to the model as JSON, where a null is a value
			// like any other and reads exactly like the "zero days left" the
			// comment set out to avoid.
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
	// Tomorrow's own line of the runway, absent when the forecast does not reach
	// it. Every other cash figure here is about the month, and asked about a
	// single day ahead the model answered with the month-end balance and the
	// whole month's scheduled expenses — R$ 20.096,97 of commitments quoted at
	// someone who had asked what tomorrow looks like.
	if next := dayCashToolPayload(a.CashPosition.NextDay); next != nil {
		payload["caixa"].(map[string]any)["amanha"] = next
	}
	if a.Projection.AccelerationPct() > 0 {
		payload["aceleracao_necessaria_pct"] = a.Projection.AccelerationPct()
	}
	// The day's ask, already scaled by the weekday. Without it the model had
	// falta_para_a_meta_na_projecao and dias_restantes_no_mes_com_hoje and did
	// the only arithmetic those two allow — divide one by the other — so a
	// pharmacy whose Sundays bring a third of a Monday was told to sell the same
	// R$ 1.200,00 on both. The system prompt forbids that division and points
	// here instead; this is the number that makes the prohibition answerable.
	//
	// Always present, and never a bare number: `situacao` carries the reason
	// when there is no ask, because "a meta já foi batida" and "não há histórico
	// para calcular" are opposite answers to the same question and an absent key
	// makes them the same silence. The amounts appear only under "ok" — a
	// "meta: 0" would be read aloud as a target of nothing.
	payload["meta_de_hoje"] = dayTargetToolPayload(a.Projection.TodayTarget)
	// Any other day is a call away, and the payload says so rather than leaving
	// the model to conclude that today is the only day this analysis can price.
	// It briefly shipped a meta_de_amanha field beside the one above, which is
	// the same mistake with one more day on it — the question after "e amanhã?"
	// is "e no sábado?". See ADR-021.
	payload["meta_de_outro_dia"] = fmt.Sprintf(
		"Para a meta de qualquer outro dia (amanhã, sábado, o dia 15), chame %s com as datas.", dayTargetToolName)
	if a.Projection.Gap > 0 {
		payload["falta_para_a_meta_na_projecao"] = reais(a.Projection.Gap)
	}
	// Present only when the balance actually crosses zero. A month whose runway
	// never dips has no such day, and the payload says so by not carrying the
	// key at all.
	if d := a.CashPosition.DaysUntilNegative; d != nil {
		payload["caixa"].(map[string]any)["dias_ate_saldo_negativo"] = *d
	}

	// The five biggest categories are a ranking, and a ranking that was cut must
	// say so — otherwise "as maiores despesas foram estas" describes a month
	// with twelve categories as though it had five (ADR-015).
	if len(a.ExpenseComposition) > maxToolCategories {
		payload["maiores_despesas_truncado"] = true
		payload["maiores_despesas_warning"] = fmt.Sprintf(
			"Mostrando as %d maiores de %d categorias. Peça a seção %q para a lista completa.",
			maxToolCategories, len(a.ExpenseComposition), SectionExpensesFull)
	}

	// What else exists, named so the model can ask for it by name rather than
	// concluding the analysis does not have it.
	index := make([]map[string]any, 0, len(sectionCatalog))
	for _, s := range sectionCatalog {
		index = append(index, map[string]any{"secao": string(s.Name), "traz": s.What})
	}
	payload["secoes_disponiveis"] = index

	for _, s := range sections {
		switch s {
		case SectionCashOutDays:
			payload[string(s)] = cashOutToolPayload(a.CashOutDays)
		case SectionExpensesFull:
			payload[string(s)] = expenseToolPayload(a.ExpenseComposition)
		case SectionHistory:
			payload[string(s)] = historyToolPayload(a.History)
		case SectionHighlights:
			payload[string(s)] = map[string]any{
				"melhor_saldo": dayText(a.Highlights.BestBalance),
				"pior_saldo":   dayText(a.Highlights.WorstBalance),
			}
		}
	}

	return payload
}

// toolOnlyPayloadKeys are the ToolPayload entries that exist only to be acted
// on: they tell a model with tools what else it could fetch and how. A reader
// that cannot call anything has no use for them, and "chame get_meta_do_dia" is
// worse than useless in a one-shot rewrite — it is an instruction the writer
// cannot follow and might repeat to the user.
var toolOnlyPayloadKeys = []string{"secoes_disponiveis", "meta_de_outro_dia"}

// DigestPayload is the insights JSON as the daily WhatsApp digest sees it: the
// same payload the bot reads, minus the affordances above.
//
// It is derived from ToolPayload rather than assembled separately, and that is
// the point. The digest and the analysis page and the bot are three renderings
// of one analysis (see the epic behind ADR-017 onward); a second hand-picked
// payload for the digest would be a fourth set of figures to keep in agreement,
// and the first field anyone forgot to add to it would go missing from the
// morning message with nothing failing.
func (a Analysis) DigestPayload() map[string]any {
	payload := a.ToolPayload()
	for _, k := range toolOnlyPayloadKeys {
		delete(payload, k)
	}
	// The truncation warning stays — a ranking that was cut has to say so, and
	// that rule does not stop applying because the reader is on WhatsApp
	// (ADR-015) — but it is reworded without the half of it that tells the
	// reader to ask for a section. Deleting it outright would leave five of
	// twelve categories presented as the whole list; leaving it whole put "peça
	// a seção despesas_completas" in front of someone with no way to do that.
	if len(a.ExpenseComposition) > maxToolCategories {
		payload["maiores_despesas_warning"] = fmt.Sprintf(
			"Mostrando as %d maiores de %d categorias.", maxToolCategories, len(a.ExpenseComposition))
	}
	return payload
}

// dayTargetToolPayload renders one day's ask, or names the reason there is
// none. The reason is the point: asked "quanto preciso vender hoje?" on a
// Sunday the shop does not open, the model must be able to say so instead of
// treating the silence as licence to divide the gap by the days left.
//
// `apuracao` is what tells a target from a result. The same three numbers read
// as a plan on a Wednesday still to come and as a fact on one that has closed,
// and a model reading them aloud has no other way to know which it is holding.
//
// The comparison against the weekday average is keyed by its subject —
// `meta_vs_media_pct`/`esforco` for a day being asked, `realizado_vs_media_pct`
// /`desempenho` for one that has closed — because the direction alone means
// opposite things on the two sides of today. A single `diferenca_pct: 12` with
// `status: "above"` is how, ten minutes after midnight on a Wednesday with
// nothing sold, the bot reported "estamos com um bom desempenho, superando a
// média histórica". It was reading a demand as an achievement: the ask is above
// a usual Wednesday precisely because the month is behind.
//
// The date travels with the ask wherever there is a day to name — including the
// cases with no amounts, because "não abre nesse dia" is about a specific day
// and the model resolves "amanhã" against a system prompt, not against this
// payload.
func dayTargetToolPayload(t DayTarget) map[string]any {
	payload := map[string]any{"situacao": string(t.State)}
	if t.Date == "" {
		return payload
	}
	payload["apuracao"] = string(t.Basis)
	payload["data"] = t.Date
	payload["dia_da_semana"] = weekdayNames[int(t.Day)]
	// What the day sold is fact, and it is reported whether or not there was a
	// goal to measure it against — a month with no meta still has days that
	// traded. It is absent only for a day that has not begun, where a zero would
	// read as a day that sold nothing.
	if t.Basis != DayProjected {
		payload["realizado"] = reais(t.Realized)
	}
	if t.Historical <= 0 {
		return payload
	}
	payload["media_historica"] = reais(t.Historical)

	// A closed day carries no target: today's plan distributes what is missing
	// now, and charging it to a day nobody can sell on any more is a goal
	// invented backwards. What it has instead is how it did.
	if t.Basis == DayRealized {
		payload["realizado_vs_media_pct"] = roundToInt(t.DeltaPercent * 100)
		payload["desempenho"] = string(t.Status)
		return payload
	}
	if t.Asked() {
		payload["meta"] = reais(t.Target)
		payload["meta_vs_media_pct"] = roundToInt(t.DeltaPercent * 100)
		payload["esforco"] = string(t.Status)
	}
	return payload
}

// dayCashToolPayload renders one day of the runway. Nil yields nothing at all
// rather than a row of zeroes, which the model would read aloud as a day with
// no money moving instead of a day the forecast does not reach.
func dayCashToolPayload(d *DayCash) map[string]any {
	if d == nil {
		return nil
	}
	return map[string]any{
		"data":               d.Date,
		"saldo_projetado":    reais(d.Balance),
		"entradas_agendadas": reais(d.ScheduledIn),
		"entradas_esperadas": reais(d.ExpectedIn),
		"despesas_agendadas": reais(d.ScheduledOut),
	}
}

func cashOutToolPayload(days []CashOutDay) []map[string]any {
	out := make([]map[string]any, 0, len(days))
	for _, d := range days {
		items := make([]map[string]any, 0, len(d.Items))
		for _, i := range d.Items {
			items = append(items, map[string]any{
				"categoria":   i.Category,
				"valor":       reais(i.Amount),
				"lancamentos": i.Count,
			})
		}
		out = append(out, map[string]any{
			"data":       d.Date,
			"total":      reais(d.Total),
			"categorias": items,
		})
	}
	return out
}

func expenseToolPayload(composition []ExpenseComposition) []map[string]any {
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

// historyToolPayload renders the trailing months. A month with no goal set
// carries no target at all rather than a zero, which the model would otherwise
// read aloud as "a meta era R$ 0,00".
func historyToolPayload(months []MonthlySnapshot) []map[string]any {
	out := make([]map[string]any, 0, len(months))
	for _, m := range months {
		row := map[string]any{
			"mes":         m.Month,
			"faturamento": reais(m.Revenue),
			"despesa":     reais(m.Expense),
		}
		if m.RevenueTarget != nil {
			row["meta_faturamento"] = reais(*m.RevenueTarget)
		}
		if m.ExpenseTarget != nil {
			row["teto_despesa"] = reais(*m.ExpenseTarget)
		}
		out = append(out, row)
	}
	return out
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

// topExpenses keeps the composition short enough to be quoted in a reply. The
// caller warns when it cut — see ToolPayload.
func topExpenses(composition []ExpenseComposition) []map[string]any {
	if len(composition) > maxToolCategories {
		composition = composition[:maxToolCategories]
	}
	return expenseToolPayload(composition)
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
			"dia":     weekdayNames[int(d.Day)],
			"media":   reais(d.Avg),
			"semanas": d.Count,
			"base":    string(d.Basis),
		})
	}
	return out
}
