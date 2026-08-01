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
	// faturamento, is worth flagging.
	expenseGrowthPct = 10
	// revenueDropPct — faturamento falling more than this against last month.
	revenueDropPct = -10
	// weekPacePct — the dead band for week-over-week pace, either direction.
	weekPacePct = 5
)

// What each problem costs the health score. A warning is a dent; a critical
// takes the month out of the green on its own.
const (
	warningPenalty  = 15
	criticalPenalty = 40
)

// buildHealth turns the month's numbers into the traffic light and the list of
// insights behind it.
//
// Everything retrospective reads `compared`, which stops at the last finished
// day — never the raw summaries. A summary covers the whole month including
// what is merely *booked* for it, and on the 1st that is every bill of the
// month weighed against sales that have not happened yet: the digest opened
// with "saúde crítica, fluxo negativo" every single month, before the shop had
// opened. What is still ahead — the pace of the week, the per-day ask — is
// reported regardless, because that is the half of the message that is
// actionable on a morning with nothing behind it.
func buildHealth(
	entries []domain.FinancialEntry,
	compared comparison,
	week WeekComparison,
	projection Projection,
) Health {
	messages := []Insight{}

	// Realized result: what actually came in minus what actually went out over
	// the days that have finished. For a closed month this is the whole month
	// and equals summary.ExpectedBalance; for a month in progress it is
	// deliberately smaller, and on its first day there is none at all.
	realized := compared.current.balance

	if !compared.clock.measurable() {
		messages = append(messages, Insight{
			Type:        InsightMonthStart,
			Severity:    SeverityInfo,
			Title:       "Mês começando",
			Description: "Ainda não há dia fechado para avaliar",
		})
	} else {
		messages = append(messages, closedDayInsights(entries, compared)...)
	}

	if week.Pace.Days > 0 && week.Pace.Previous != 0 {
		weekPct := percentChange(week.Pace.Current, week.Pace.Previous)
		switch {
		case weekPct > weekPacePct:
			messages = append(messages, Insight{
				Type:        InsightWeeklyImprovement,
				Severity:    SeverityInfo,
				Title:       "Ritmo subiu vs semana passada",
				Description: fmt.Sprintf("%d%% acima até ontem", roundToInt(weekPct)),
				Value:       ptr(weekPct),
			})
		case weekPct < -weekPacePct:
			messages = append(messages, Insight{
				Type:        InsightWeeklyDecline,
				Severity:    SeverityWarning,
				Title:       "Ritmo caiu vs semana passada",
				Description: fmt.Sprintf("%d%% abaixo até ontem", roundToInt(-weekPct)),
				Value:       ptr(weekPct),
			})
		}
	}

	if projection.Pacing() {
		if projection.OnTrack {
			description := "Faturamento já superou a meta"
			if projection.NeededPerDay > 0 {
				description = fmt.Sprintf("Necessário %s/dia — a projeção passa da meta", formatBRL(projection.NeededPerDay))
			}
			messages = append(messages, Insight{
				Type:        InsightGoalOnTrack,
				Severity:    SeverityInfo,
				Title:       "No ritmo para bater a meta",
				Description: description,
			})
		} else if projection.NeededPerDay > 0 {
			messages = append(messages, Insight{
				Type:     InsightGoalBehind,
				Severity: SeverityWarning,
				Title:    "Precisa acelerar para bater a meta",
				Description: fmt.Sprintf("Necessário %s/dia nos próximos %s",
					formatBRL(projection.NeededPerDay), pluralDias(projection.DaysRemaining)),
				Value: ptr(float64(projection.NeededPerDay)),
			})
		}
	}

	return Health{
		Status:   healthStatus(realized, messages),
		Score:    healthScore(messages),
		Messages: messages,
	}
}

// closedDayInsights are the readings that only mean something once a day of the
// month has finished: how the result stands, how the days went, and how the
// month compares with the one before it. All of them are measured over
// `compared`'s window, so they agree with each other and with the percentages
// printed beside them.
func closedDayInsights(entries []domain.FinancialEntry, compared comparison) []Insight {
	messages := []Insight{}
	current := compared.current

	if current.balance > 0 {
		messages = append(messages, Insight{
			Type:        InsightGoodPerformance,
			Severity:    SeverityInfo,
			Title:       "Resultado positivo",
			Description: "Entradas maiores que despesas" + compared.suffix(),
		})
	}

	if positiveDays, totalDays := countDays(entries, compared.clock.through); totalDays > 0 {
		// "com movimento" is not padding: the denominator is days with any
		// entry, while the weekday averages shown alongside count only days
		// with faturamento. Two different totals sat next to each other
		// unlabelled and read like an arithmetic error.
		messages = append(messages, Insight{
			Type:        InsightGoodPerformance,
			Severity:    SeverityInfo,
			Title:       fmt.Sprintf("%d dos %d dias com movimento", positiveDays, totalDays),
			Description: "Fecharam no azul",
		})
	}

	// Expenses as a share of *faturamento*, not of entradas de caixa: this is a
	// performance reading ("how much of what we sell goes out again"), and
	// dividing by cash in would let a loan flatter the ratio — borrow enough
	// and the pharmacy looks efficient.
	if current.revenue > 0 {
		pct := roundToInt(float64(current.expense) / float64(current.revenue) * 100)
		messages = append(messages, Insight{
			Type:        InsightGoodPerformance,
			Severity:    SeverityInfo,
			Title:       fmt.Sprintf("Despesas representam %d%%", pct),
			Description: "do faturamento" + compared.suffix(),
		})
	}

	// Month-over-month only says anything when both months actually sold
	// something up to this point; a percentage against a month with no sales is
	// noise.
	//
	// Measured on faturamento, not on cash in: both insights below are
	// performance readings. A month propped up by a loan must still report that
	// sales fell, and expenses outgrowing borrowed money is not the warning
	// anyone means by "despesas cresceram".
	if compared.previous.revenue > 0 && current.revenue > 0 {
		revenueChange := percentChange(current.revenue, compared.previous.revenue)
		var expenseChange float64
		if compared.previous.expense > 0 {
			expenseChange = percentChange(current.expense, compared.previous.expense)
		}

		// Expenses growing is only a problem when sales are not keeping up.
		if expenseChange > expenseGrowthPct && revenueChange < expenseChange {
			messages = append(messages, Insight{
				Type:        InsightExpenseGrowth,
				Severity:    SeverityWarning,
				Title:       "Despesas cresceram",
				Description: fmt.Sprintf("%d%% acima do mês passado%s", roundToInt(expenseChange), compared.suffix()),
				Value:       ptr(expenseChange),
			})
		}

		if revenueChange < revenueDropPct {
			messages = append(messages, Insight{
				Type:        InsightRevenueDrop,
				Severity:    SeverityWarning,
				Title:       "Faturamento caiu",
				Description: fmt.Sprintf("%d%% abaixo do mês passado%s", roundToInt(-revenueChange), compared.suffix()),
				Value:       ptr(revenueChange),
			})
		}
	}

	if current.balance < 0 {
		messages = append(messages, Insight{
			Type:        InsightLowCashFlow,
			Severity:    SeverityCritical,
			Title:       "Fluxo negativo",
			Description: "Saídas maiores que entradas" + compared.suffix(),
		})
	}

	return messages
}

// healthScore is the number printed next to the traffic light.
//
// The dashboard used to compute it itself, as the share of insights that were
// merely informational. That made the score a function of how many rules
// happened to fire rather than of the month: a good month that picked up one
// more cheerful insight moved the score, a critical and a mild warning weighed
// the same, and the number could disagree with the status beside it. Here a
// clean month is 100 and every problem is priced by its severity.
func healthScore(messages []Insight) int {
	score := 100
	for _, m := range messages {
		switch m.Severity {
		case SeverityWarning:
			score -= warningPenalty
		case SeverityCritical:
			score -= criticalPenalty
		}
	}
	return max(0, score)
}

// countDays returns how many days closed in the black, and how many days saw
// any entry at all, over the days of the month that have finished. A day is in
// the black when everything that came in that day beats everything that went
// out — a true cash balance, not a sales figure, so every inflow counts here,
// including the ones that are not faturamento. A loan really did keep the day
// out of the red.
//
// Days are the entries' effective dates, the same field every other total on
// this page is bucketed by, and days past throughDay are left out: bills booked
// for later in the month are not days that "went by without movimento", and
// counting them made a fresh month read as "0 dos 8 dias no azul".
func countDays(entries []domain.FinancialEntry, throughDay int) (positive, total int) {
	byDate := map[string]int64{}
	for _, e := range entries {
		effective := pkgfinance.EffectiveDate(e)
		if effective.Day() > throughDay {
			continue
		}
		date := effective.Format("2006-01-02")
		if e.Type == domain.EntryTypeIncome {
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
// realized balance is critical on its own, regardless of how many cheerful
// insights came before it. balance is the result over the days that have
// finished — a month whose bills are merely *booked* has not gone critical yet.
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
