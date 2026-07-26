package analytics

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// day parses a "YYYY-MM-DD" test fixture date, failing the test rather than
// silently producing a zero date.
func day(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func sale(t *testing.T, date string, amount int64) domain.FinancialEntry {
	t.Helper()
	return domain.FinancialEntry{
		TransactionDate: day(t, date),
		Amount:          amount,
		Type:            domain.EntryTypeIncome,
		Category:        "venda_balcao",
	}
}

func expense(t *testing.T, date, category string, amount int64) domain.FinancialEntry {
	t.Helper()
	return domain.FinancialEntry{
		TransactionDate: day(t, date),
		Amount:          amount,
		Type:            domain.EntryTypeExpense,
		Category:        category,
	}
}

func at12(t *testing.T, date string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("parse now %q: %v", date, err)
	}
	return parsed.Add(12 * time.Hour)
}

func summary(income, expense int64) *pkgfinance.MonthlySummary {
	return &pkgfinance.MonthlySummary{TotalIncome: income, TotalExpense: expense, Balance: income - expense}
}

func TestBuildTrend(t *testing.T) {
	tests := []struct {
		name              string
		current, previous int64
		wantChange        int
		wantDirection     TrendDirection
	}{
		{"growth", 12000, 10000, 20, TrendUp},
		{"decline", 8000, 10000, -20, TrendDown},
		{"inside the dead band reads as stable", 10100, 10000, 1, TrendStable},
		{"no previous month is 100% up", 5000, 0, 100, TrendUp},
		{"no previous month and nothing now is stable", 0, 0, 0, TrendStable},
		{"recovering from a negative balance reads as up", 500, -1000, 150, TrendUp},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildTrend(tc.current, tc.previous)
			if got.Change != tc.wantChange || got.Direction != tc.wantDirection {
				t.Errorf("buildTrend(%d, %d) = %d%%/%s, want %d%%/%s",
					tc.current, tc.previous, got.Change, got.Direction, tc.wantChange, tc.wantDirection)
			}
		})
	}
}

func TestWeekdayStatsAveragesOverDistinctDays(t *testing.T) {
	// Two sales on the same Monday, one on the next — the Monday average is
	// over two Mondays, not three sales.
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-06", 10000),
		sale(t, "2026-07-06", 20000),
		sale(t, "2026-07-13", 30000),
		expense(t, "2026-07-06", "aluguel", 90000),
	}

	stats := weekdayStats(entries, at12(t, "2026-07-15")) // a Wednesday

	monday := stats[1]
	if monday.Label != "Seg" {
		t.Fatalf("index 1 should be Monday, got %q", monday.Label)
	}
	if monday.Count != 2 {
		t.Errorf("Count = %d, want 2 distinct Mondays", monday.Count)
	}
	if monday.Total != 60000 {
		t.Errorf("Total = %d, want 60000 (expenses must not count)", monday.Total)
	}
	if monday.Avg != 30000 {
		t.Errorf("Avg = %d, want 30000", monday.Avg)
	}
	if monday.IsToday {
		t.Error("Monday should not be today when now is a Wednesday")
	}
	if !stats[3].IsToday {
		t.Error("Wednesday should be flagged as today")
	}
}

func TestHighlightsWithoutEntries(t *testing.T) {
	h := buildHighlights(nil)
	if h.BestIncome.Label != "Sem dados" || h.WorstBalance.Date != "—" {
		t.Errorf("empty highlights = %+v, want the Sem dados placeholder in every slot", h)
	}
}

func TestHighlightsPickBestAndWorstDays(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 50000),
		sale(t, "2026-07-02", 10000),
		expense(t, "2026-07-02", "fornecedor_geral", 5000),
		sale(t, "2026-07-03", 20000),
		expense(t, "2026-07-03", "aluguel", 90000),
	}

	h := buildHighlights(entries)

	if h.BestIncome.Date != "2026-07-01" || h.BestIncome.Amount != 50000 {
		t.Errorf("BestIncome = %+v, want 2026-07-01 / 50000", h.BestIncome)
	}
	if h.WorstIncome.Date != "2026-07-02" {
		t.Errorf("WorstIncome = %+v, want 2026-07-02", h.WorstIncome)
	}
	if h.BestBalance.Date != "2026-07-01" || h.BestBalance.Amount != 50000 {
		t.Errorf("BestBalance = %+v, want 2026-07-01 / 50000", h.BestBalance)
	}
	// 20000 in, 90000 out.
	if h.WorstBalance.Date != "2026-07-03" || h.WorstBalance.Amount != -70000 {
		t.Errorf("WorstBalance = %+v, want 2026-07-03 / -70000", h.WorstBalance)
	}
	if h.BestIncome.Label != "01 de jul." {
		t.Errorf("Label = %q, want %q", h.BestIncome.Label, "01 de jul.")
	}
}

func TestCashOutDaysRankedAndCapped(t *testing.T) {
	var entries []domain.FinancialEntry
	// Six spending days, each heavier than the last.
	for i := 1; i <= 6; i++ {
		entries = append(entries, expense(t, time.Date(2026, 7, i, 0, 0, 0, 0, time.UTC).Format("2006-01-02"),
			"fornecedor_geral", int64(i)*1000))
	}
	entries = append(
		entries,
		expense(t, "2026-07-06", "aluguel", 500),
		expense(t, "2026-07-06", "aluguel", 500),
		sale(t, "2026-07-06", 999999), // income must not appear
	)

	days := buildCashOutDays(entries)

	if len(days) != maxCashOutDays {
		t.Fatalf("got %d days, want %d", len(days), maxCashOutDays)
	}
	if days[0].Date != "2026-07-06" || days[0].Total != 7000 {
		t.Errorf("heaviest day = %+v, want 2026-07-06 / 7000", days[0])
	}
	if len(days[0].Items) != 2 || days[0].Items[0].Category != "fornecedor_geral" {
		t.Errorf("items = %+v, want fornecedor_geral first", days[0].Items)
	}
	if days[0].Items[1].Count != 2 {
		t.Errorf("aluguel Count = %d, want the two entries folded together", days[0].Items[1].Count)
	}
	if days[len(days)-1].Date != "2026-07-02" {
		t.Errorf("lightest kept day = %q, want the cheapest day to have been dropped", days[len(days)-1].Date)
	}
}

func TestExpenseComposition(t *testing.T) {
	entries := []domain.FinancialEntry{
		expense(t, "2026-07-01", "aluguel", 60000),
		expense(t, "2026-07-02", "folha_pagamento", 30000),
		expense(t, "2026-07-03", "folha_pagamento", 10000),
		sale(t, "2026-07-03", 999999),
	}

	got := expenseComposition(entries)

	if len(got) != 2 {
		t.Fatalf("got %d categories, want 2", len(got))
	}
	if got[0].CategoryID != "aluguel" || got[0].Percentage != 60 {
		t.Errorf("first = %+v, want aluguel at 60%%", got[0])
	}
	if got[1].CategoryName != "Folha de Pagamento" {
		t.Errorf("CategoryName = %q, want the domain label", got[1].CategoryName)
	}
	if len(expenseComposition(nil)) != 0 {
		t.Error("no expenses should yield no composition, not a division by zero")
	}
}

func TestCategoryLabelFallsBackToTitleCase(t *testing.T) {
	if got := categoryLabel("taxa_maquininha"); got != "Taxa Maquininha" {
		t.Errorf("categoryLabel(unknown slug) = %q, want %q", got, "Taxa Maquininha")
	}
}

func TestGoalProgress(t *testing.T) {
	now := at12(t, "2026-07-10") // July has 31 days
	sum := pkgfinance.MonthlySummary{TotalIncome: 50000, TotalExpense: 30000}

	t.Run("without a goal", func(t *testing.T) {
		got := goalProgress(sum, nil, now, 40000)
		if got.RevenueTarget != 0 || got.RevenuePct != 0 {
			t.Errorf("targets = %+v, want zeroes with no goal set", got)
		}
		if got.RevenueActual != 40000 {
			t.Errorf("RevenueActual = %d, want the counter-sales total", got.RevenueActual)
		}
		if got.DaysRemaining != 21 || got.DaysTotal != 31 {
			t.Errorf("days = %d/%d, want 21/31", got.DaysRemaining, got.DaysTotal)
		}
	})

	t.Run("percentages cap at 100", func(t *testing.T) {
		goal := &domain.Goal{RevenueTarget: 20000, ExpenseTarget: 60000}
		got := goalProgress(sum, goal, now, 40000)
		if got.RevenuePct != 100 {
			t.Errorf("RevenuePct = %d, want it capped at 100", got.RevenuePct)
		}
		if got.ExpensePct != 50 {
			t.Errorf("ExpensePct = %d, want 50", got.ExpensePct)
		}
	})
}

func TestBuildHistoryKeepsMonthsAligned(t *testing.T) {
	months := []string{"2026-05", "2026-06", "2026-07"}
	// June has no data at all — it must stay in place as a zero bar.
	summaries := []*pkgfinance.MonthlySummary{summary(1000, 500), nil, summary(3000, 2000)}
	goals := []*domain.Goal{nil, nil, {RevenueTarget: 5000, ExpenseTarget: 4000}}

	got := buildHistory(months, summaries, goals)

	if len(got) != 3 {
		t.Fatalf("got %d snapshots, want 3", len(got))
	}
	if got[1].Month != "2026-06" || got[1].Income != 0 {
		t.Errorf("middle month = %+v, want an empty 2026-06 in place", got[1])
	}
	if got[0].IncomeTarget != nil {
		t.Errorf("IncomeTarget = %v, want nil for a month with no goal", *got[0].IncomeTarget)
	}
	if got[2].IncomeTarget == nil || *got[2].IncomeTarget != 5000 {
		t.Errorf("last IncomeTarget = %v, want 5000", got[2].IncomeTarget)
	}
	if got[2].Label != "jul. de 2026" {
		t.Errorf("Label = %q, want %q", got[2].Label, "jul. de 2026")
	}
}

func TestCashPosition(t *testing.T) {
	now := at12(t, "2026-07-10")

	t.Run("no projection", func(t *testing.T) {
		got := buildCashPosition(nil, now)
		if got.DaysUntilNegative != nil || got.LowestProjectedDate != "2026-07-10" {
			t.Errorf("empty position = %+v, want zeroes anchored to today", got)
		}
	})

	t.Run("finds the crossing and the trough", func(t *testing.T) {
		points := []pkgfinance.CashFlowPoint{
			{Date: "2026-07-09", RunningBalance: 20000},
			{Date: "2026-07-10", RunningBalance: 15000},
			{Date: "2026-07-13", RunningBalance: -5000},
			{Date: "2026-07-14", RunningBalance: -9000},
			{Date: "2026-07-31", RunningBalance: 2000},
		}
		got := buildCashPosition(points, now)

		if got.CurrentBalance != 15000 {
			t.Errorf("CurrentBalance = %d, want today's 15000", got.CurrentBalance)
		}
		if got.EndOfMonthProjection != 2000 {
			t.Errorf("EndOfMonthProjection = %d, want the last point's 2000", got.EndOfMonthProjection)
		}
		if got.DaysUntilNegative == nil || *got.DaysUntilNegative != 3 {
			t.Errorf("DaysUntilNegative = %v, want 3", got.DaysUntilNegative)
		}
		if got.LowestProjected != -9000 || got.LowestProjectedDate != "2026-07-14" {
			t.Errorf("trough = %d on %s, want -9000 on 2026-07-14", got.LowestProjected, got.LowestProjectedDate)
		}
	})

	t.Run("a balance already negative today is not a future crossing", func(t *testing.T) {
		points := []pkgfinance.CashFlowPoint{{Date: "2026-07-10", RunningBalance: -100}}
		if got := buildCashPosition(points, now); got.DaysUntilNegative != nil {
			t.Errorf("DaysUntilNegative = %v, want nil — the crossing has already happened", *got.DaysUntilNegative)
		}
	})
}

func TestWeekComparison(t *testing.T) {
	// Wednesday 2026-07-15. This week starts Monday the 13th; last week ran
	// Monday the 6th to Sunday the 12th, and "up to the same day" is the 8th.
	now := at12(t, "2026-07-15")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-13", 10000),
		sale(t, "2026-07-15", 10000),
		sale(t, "2026-07-16", 99999), // tomorrow — must not count
		sale(t, "2026-07-06", 5000),
		sale(t, "2026-07-08", 5000),
		sale(t, "2026-07-12", 20000), // last Sunday: in Previous, not PreviousUpToDay
	}

	got := buildWeekComparison(entries, now, 100000, 500000)

	if got.Current != 20000 {
		t.Errorf("Current = %d, want 20000", got.Current)
	}
	if got.Previous != 30000 {
		t.Errorf("Previous = %d, want the whole of last week (30000)", got.Previous)
	}
	if got.PreviousUpToDay != 10000 {
		t.Errorf("PreviousUpToDay = %d, want last Mon–Wed (10000)", got.PreviousUpToDay)
	}
	if want := []string{"Seg", "Ter", "Qua"}; len(got.Labels) != len(want) {
		t.Errorf("Labels = %v, want one per elapsed day this week (%v)", got.Labels, want)
	}
	// 20000 so far + 4 remaining days at last week's 30000/7 per day, to the
	// nearest centavo.
	if want := int64(37143); got.ProjectedWeekly != want {
		t.Errorf("ProjectedWeekly = %d, want %d", got.ProjectedWeekly, want)
	}
	if got.MonthlyTarget != 500000 {
		t.Errorf("MonthlyTarget = %d, want it carried through", got.MonthlyTarget)
	}
}

func TestWeekComparisonTreatsSundayAsTheEndOfItsWeek(t *testing.T) {
	// Sunday 2026-07-19 belongs to the week that began Monday the 13th.
	now := at12(t, "2026-07-19")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-13", 10000),
		sale(t, "2026-07-19", 10000),
		sale(t, "2026-07-20", 99999), // next Monday
	}

	got := buildWeekComparison(entries, now, 0, 0)

	if got.Current != 20000 {
		t.Errorf("Current = %d, want Monday-through-Sunday (20000)", got.Current)
	}
	if len(got.Labels) != 7 {
		t.Errorf("Labels = %v, want all seven days by Sunday", got.Labels)
	}
	// The week is over, so there is nothing left to project into it.
	if got.ProjectedWeekly != got.Current {
		t.Errorf("ProjectedWeekly = %d, want no projection past a finished week (%d)", got.ProjectedWeekly, got.Current)
	}
}

func TestHealthStatusEscalates(t *testing.T) {
	tests := []struct {
		name     string
		balance  int64
		messages []Insight
		want     HealthStatus
	}{
		{"nothing wrong", 100, []Insight{{Severity: SeverityInfo}}, HealthBoa},
		{"a warning is attention", 100, []Insight{{Severity: SeverityWarning}}, HealthAtencao},
		{
			"a critical outranks a later warning", 100,
			[]Insight{{Severity: SeverityCritical}, {Severity: SeverityWarning}},
			HealthCritico,
		},
		{"a negative balance is critical on its own", -1, []Insight{{Severity: SeverityInfo}}, HealthCritico},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := healthStatus(tc.balance, tc.messages); got != tc.want {
				t.Errorf("healthStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHealthFlagsRevenueDropAndExpenseGrowth(t *testing.T) {
	current := pkgfinance.MonthlySummary{TotalIncome: 80000, TotalExpense: 60000, Balance: 20000}
	// Two closed months, so the comparison is whole-against-whole.
	compared := comparison{
		current:  monthTotals{income: 80000, expense: 60000, balance: 20000},
		previous: monthTotals{income: 100000, expense: 40000, balance: 60000},
	}

	health := buildHealth(nil, current, compared, WeekComparison{}, GoalProgress{})

	byType := map[InsightType]Insight{}
	for _, m := range health.Messages {
		byType[m.Type] = m
	}

	drop, ok := byType[InsightRevenueDrop]
	if !ok {
		t.Fatalf("expected a revenue-drop insight, got %+v", health.Messages)
	}
	if drop.Description != "20% abaixo do mês passado" {
		t.Errorf("description = %q, want the drop as a positive percentage", drop.Description)
	}
	if growth, ok := byType[InsightExpenseGrowth]; !ok {
		t.Errorf("expected an expense-growth insight, got %+v", health.Messages)
	} else if growth.Description != "50% acima do mês passado" {
		t.Errorf("description = %q", growth.Description)
	}
	if health.Status != HealthAtencao {
		t.Errorf("status = %q, want atencao", health.Status)
	}
}

func TestHealthGoalPaceMessages(t *testing.T) {
	// 10 of 30 days gone, R$1.000,00 of a R$10.000,00 target — the rate so far
	// (R$100/day) is far below the R$450/day still needed.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 100000, DaysTotal: 30, DaysRemaining: 20}
	health := buildHealth(nil, pkgfinance.MonthlySummary{}, comparison{}, WeekComparison{}, goals)

	var behind *Insight
	for i, m := range health.Messages {
		if m.Type == InsightGoalBehind {
			behind = &health.Messages[i]
		}
	}
	if behind == nil {
		t.Fatalf("expected a goal-behind insight, got %+v", health.Messages)
	}
	if want := "Necessário R$ 450,00/dia nos próximos 20 dias"; behind.Description != want {
		t.Errorf("description = %q, want %q", behind.Description, want)
	}
}

func TestHealthCountsPositiveDays(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 10000),
		sale(t, "2026-07-02", 1000),
		expense(t, "2026-07-02", "aluguel", 5000),
	}

	positive, total := countDays(entries)

	if positive != 1 || total != 2 {
		t.Errorf("countDays = %d of %d, want 1 of 2", positive, total)
	}
}

func TestRecommendationsWeeklyPaceMatrix(t *testing.T) {
	// 10 of 30 days gone; the actual chosen below decides "on track".
	base := GoalProgress{RevenueTarget: 1000000, DaysTotal: 30, DaysRemaining: 20}

	// On track: R$5.000,00 in 10 days is R$500/day against the R$250/day still
	// needed. Behind: R$1.000,00 in 10 days is R$100/day against R$450/day.
	onTrack := base
	onTrack.RevenueActual = 500000
	behind := base
	behind.RevenueActual = 100000

	improved := WeekComparison{Current: 12000, PreviousUpToDay: 10000}
	declined := WeekComparison{Current: 8000, PreviousUpToDay: 10000}
	stable := WeekComparison{Current: 10000, PreviousUpToDay: 10000}

	tests := []struct {
		name  string
		week  WeekComparison
		goals GoalProgress
		want  string
	}{
		{"up and closing", improved, onTrack, "Ritmo subiu e fecha a meta"},
		{"up but short", improved, behind, "Ritmo subiu mas ainda falta"},
		{"down but closing", declined, onTrack, "Caiu mas a projeção fecha"},
		{"down and short", declined, behind, "Faturamento caiu e não bate a meta"},
		{"flat and closing", stable, onTrack, "Ritmo estável e dentro da projeção"},
		{"flat and short", stable, behind, "Ritmo estável mas não é suficiente"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recs := buildRecommendations(tc.week, tc.goals, Trends{}, CashPosition{})
			if len(recs) == 0 {
				t.Fatal("expected a weekly recommendation")
			}
			if recs[0].Title != tc.want {
				t.Errorf("title = %q, want %q", recs[0].Title, tc.want)
			}
		})
	}
}

func TestRecommendationsWithoutAGoal(t *testing.T) {
	recs := buildRecommendations(WeekComparison{}, GoalProgress{}, Trends{}, CashPosition{})
	if len(recs) != 0 {
		t.Errorf("recommendations = %+v, want none when there is nothing to pace against", recs)
	}
}

func TestRecommendationsFlagTrendsAndRunway(t *testing.T) {
	trends := Trends{
		Receita: MonthTrend{Change: -25, Direction: TrendDown},
		Despesa: MonthTrend{Change: 40, Direction: TrendUp},
	}
	oneDay := 1
	cash := CashPosition{DaysUntilNegative: &oneDay}

	recs := buildRecommendations(WeekComparison{}, GoalProgress{}, trends, cash)

	titles := make([]string, len(recs))
	for i, r := range recs {
		titles[i] = r.Title
	}
	want := []string{"Despesas acima do normal", "Receita caiu", "Saldo fica negativo em breve"}
	if len(titles) != len(want) {
		t.Fatalf("recommendations = %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("recommendation %d = %q, want %q", i, titles[i], want[i])
		}
	}
	if recs[2].Message != "O saldo fica negativo em 1 dia. Reduza despesas ou antecipe recebimentos." {
		t.Errorf("runway message = %q, want the singular day form", recs[2].Message)
	}
}

func TestFormatBRL(t *testing.T) {
	tests := []struct {
		centavos int64
		want     string
	}{
		{0, "R$ 0,00"},
		{5, "R$ 0,05"},
		{123456, "R$ 1.234,56"},
		{100000000, "R$ 1.000.000,00"},
		{-2550, "-R$ 25,50"},
	}
	for _, tc := range tests {
		if got := formatBRL(tc.centavos); got != tc.want {
			t.Errorf("formatBRL(%d) = %q, want %q", tc.centavos, got, tc.want)
		}
	}
}

func TestMonthRangeAndBounds(t *testing.T) {
	got := MonthRange("2026-01", 3)
	want := []string{"2025-11", "2025-12", "2026-01"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MonthRange = %v, want %v", got, want)
		}
	}

	from, to, err := MonthBounds("2026-02")
	if err != nil {
		t.Fatalf("MonthBounds: %v", err)
	}
	if from.Format("2006-01-02") != "2026-02-01" || to.Format("2006-01-02") != "2026-02-28" {
		t.Errorf("bounds = %s..%s, want the whole of February", from, to)
	}
	if _, _, err := MonthBounds("julho"); err == nil {
		t.Error("expected an error for a malformed month")
	}
}

func TestBuildProducesAWholeAnalysis(t *testing.T) {
	now := at12(t, "2026-07-15")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 200000),
		sale(t, "2026-07-13", 100000),
		sale(t, "2026-07-15", 150000),
		expense(t, "2026-07-05", "aluguel", 300000),
	}
	previous := []domain.FinancialEntry{
		sale(t, "2026-06-10", 400000),
		sale(t, "2026-06-25", 900000), // past today's day-of-month, so excluded
	}

	got := Build(Input{
		Month:           "2026-07",
		Entries:         entries,
		PreviousEntries: previous,
		Summaries: []*pkgfinance.MonthlySummary{
			summary(100000, 50000), summary(1300000, 400000), summary(450000, 300000),
		},
		Goals:          []*domain.Goal{nil, nil, {RevenueTarget: 1000000, ExpenseTarget: 500000}},
		CashFlowPoints: []pkgfinance.CashFlowPoint{{Date: "2026-07-15", RunningBalance: 150000}},
		Now:            now,
	})

	if got.Month != "2026-07" {
		t.Errorf("Month = %q", got.Month)
	}
	if got.KPIs.Receita != 450000 || got.KPIs.Despesa != 300000 || got.KPIs.Resultado != 150000 {
		t.Errorf("KPIs = %+v, want the analysed month's summary", got.KPIs)
	}
	if got.KPIs.DaysRemaining != 16 {
		t.Errorf("DaysRemaining = %d, want 16", got.KPIs.DaysRemaining)
	}
	if got.KPIs.PreviousMonthIncomeUpToDay != 400000 {
		t.Errorf("PreviousMonthIncomeUpToDay = %d, want last month truncated at day 15", got.KPIs.PreviousMonthIncomeUpToDay)
	}
	if got.Goals.RevenueActual != 450000 {
		t.Errorf("Goals.RevenueActual = %d, want the counter-sales total", got.Goals.RevenueActual)
	}
	// July is genuinely *ahead* at the same height of the month: 450.000 by
	// the 15th against June's 400.000 by its 15th. Comparing against June's
	// closed 1.300.000 would call that a collapse, which is the whole point
	// of measuring both months to the same day.
	if got.Trends.Receita.Direction != TrendUp {
		t.Errorf("Receita trend = %+v, want up", got.Trends.Receita)
	}
	if len(got.History) != HistoryMonths || got.History[0].Month != "2026-05" {
		t.Errorf("History = %+v, want a trailing three-month window", got.History)
	}
	if len(got.Weekdays) != 7 {
		t.Errorf("Weekdays = %d entries, want 7", len(got.Weekdays))
	}
	if len(got.Recommendations) == 0 {
		t.Error("expected at least the weekly recommendation")
	}
}

func TestBuildToleratesAnEmptyMonth(t *testing.T) {
	got := Build(Input{Month: "2026-07", Now: at12(t, "2026-07-15")})

	if got.Health.Status != HealthBoa {
		t.Errorf("status = %q, want boa for a month with nothing in it", got.Health.Status)
	}
	if got.Highlights.BestIncome.Label != "Sem dados" {
		t.Errorf("highlights = %+v, want the placeholder", got.Highlights)
	}

	// The dashboard iterates every collection, so none of them may be null.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"weekdays", "cashOutDays", "expenseComposition", "history", "recommendations"} {
		if string(decoded[key]) == "null" {
			t.Errorf("%s serialized as null, want an empty array", key)
		}
	}
	var health struct {
		Messages []Insight `json:"messages"`
	}
	if err := json.Unmarshal(decoded["health"], &health); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}
	if health.Messages == nil {
		t.Error("health.messages serialized as null, want an empty array")
	}
}

func TestDigestLinesKeepOnlyWhatIsWorthSaying(t *testing.T) {
	analysis := Analysis{
		Health: Health{
			Status: HealthAtencao,
			Messages: []Insight{
				{Severity: SeverityInfo, Title: "Resultado positivo", Description: "tudo certo"},
				{Severity: SeverityWarning, Title: "Receitas cairam", Description: "17% abaixo do mês passado"},
			},
		},
		Recommendations: []Recommendation{{Title: "Receita caiu", Message: "Aja rapidamente."}},
	}

	lines := analysis.DigestLines()

	want := []string{
		"Saúde do mês: Atenção.",
		"Receitas cairam — 17% abaixo do mês passado.",
		"Receita caiu: Aja rapidamente.",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestToolPayloadUsesReais(t *testing.T) {
	analysis := Build(Input{
		Month:     "2026-07",
		Entries:   []domain.FinancialEntry{sale(t, "2026-07-01", 123456)},
		Summaries: []*pkgfinance.MonthlySummary{nil, nil, summary(123456, 0)},
		Now:       at12(t, "2026-07-15"),
	})

	payload := analysis.ToolPayload()

	if payload["receita"] != 1234.56 {
		t.Errorf("receita = %v, want reais not centavos", payload["receita"])
	}
	if payload["month"] != "2026-07" {
		t.Errorf("month = %v", payload["month"])
	}
	caixa := payload["caixa"].(map[string]any)
	if caixa["dias_ate_saldo_negativo"] != nil {
		t.Errorf("dias_ate_saldo_negativo = %v, want nil when the balance never goes negative", caixa["dias_ate_saldo_negativo"])
	}
}

func TestComparisonMeasuresBothMonthsAtTheSameHeight(t *testing.T) {
	// A month in progress: today is the 10th, and both months carry entries
	// after it. Only the first ten days of each may count.
	now := at12(t, "2026-07-10")
	current := []domain.FinancialEntry{
		sale(t, "2026-07-05", 40000),
		sale(t, "2026-07-25", 60000), // later this month
	}
	previous := []domain.FinancialEntry{
		sale(t, "2026-06-05", 50000),
		sale(t, "2026-06-25", 900000), // the rest of a closed month
	}

	got := buildComparison("2026-07", current, previous, now)

	if got.throughDay != 10 {
		t.Errorf("throughDay = %d, want 10", got.throughDay)
	}
	if got.current.income != 40000 {
		t.Errorf("current income = %d, want only the first ten days (40000)", got.current.income)
	}
	if got.previous.income != 50000 {
		t.Errorf("previous income = %d, want only the first ten days (50000)", got.previous.income)
	}
}

func TestComparisonUsesWholeMonthsForAClosedMonth(t *testing.T) {
	// Analysing June while it is July: June is over, so cutting it at the
	// 10th would be arbitrary.
	now := at12(t, "2026-07-10")
	june := []domain.FinancialEntry{sale(t, "2026-06-05", 50000), sale(t, "2026-06-25", 90000)}
	may := []domain.FinancialEntry{sale(t, "2026-05-28", 70000)}

	got := buildComparison("2026-06", june, may, now)

	if got.throughDay != 0 {
		t.Errorf("throughDay = %d, want 0 for a closed month", got.throughDay)
	}
	if got.current.income != 140000 || got.previous.income != 70000 {
		t.Errorf("totals = %d vs %d, want both months whole", got.current.income, got.previous.income)
	}
}

func TestComparisonBucketsByEffectiveDate(t *testing.T) {
	// A bill registered on the 2nd but due on the 20th belongs to the 20th —
	// the same day the stored monthly summary counts it on. Bucketing it by
	// its registration date would make the comparison disagree with the KPIs
	// shown beside it.
	due := day(t, "2026-07-20")
	bill := domain.FinancialEntry{
		TransactionDate: day(t, "2026-07-02"),
		DueDate:         &due,
		Amount:          30000,
		Type:            domain.EntryTypeExpense,
		Category:        "aluguel",
	}

	if got := totalsThroughDay([]domain.FinancialEntry{bill}, 10); got.expense != 0 {
		t.Errorf("expense = %d, want 0 — the bill is due after day 10", got.expense)
	}
	if got := totalsThroughDay([]domain.FinancialEntry{bill}, 0); got.expense != 30000 {
		t.Errorf("expense = %d, want the whole month to include it", got.expense)
	}
}

func TestPartialMonthNoLongerReadsAsACollapse(t *testing.T) {
	// The bug this fixes: on the 10th, a month trading at exactly last
	// month's pace was compared against last month's *closed* total and
	// reported as a ~67% fall — every month, for the first three weeks.
	now := at12(t, "2026-07-10")
	var current, previous []domain.FinancialEntry
	for d := 1; d <= 10; d++ {
		current = append(current, sale(t, fmt.Sprintf("2026-07-%02d", d), 10000))
	}
	for d := 1; d <= 30; d++ {
		previous = append(previous, sale(t, fmt.Sprintf("2026-06-%02d", d), 10000))
	}

	got := Build(Input{
		Month:           "2026-07",
		Entries:         current,
		PreviousEntries: previous,
		Summaries: []*pkgfinance.MonthlySummary{
			nil, summary(300000, 0), summary(100000, 0),
		},
		Now: now,
	})

	if got.Trends.Receita.Direction != TrendStable || got.Trends.Receita.Change != 0 {
		t.Errorf("Receita trend = %+v, want stable at 0%% — same pace, same height of month",
			got.Trends.Receita)
	}
	if got.Trends.ComparedThroughDay != 10 {
		t.Errorf("ComparedThroughDay = %d, want 10 so the UI can label the window",
			got.Trends.ComparedThroughDay)
	}
	// And the false alarms that fell out of it are gone.
	for _, r := range got.Recommendations {
		if r.Title == "Receita caiu" {
			t.Errorf("still recommending %q for a month trading at last month's pace", r.Title)
		}
	}
	for _, m := range got.Health.Messages {
		if m.Type == InsightRevenueDrop {
			t.Errorf("still flagging a revenue drop: %+v", m)
		}
	}
}

func TestPartialMonthStillReportsARealDrop(t *testing.T) {
	// Half last month's pace over the same ten days is a real fall and must
	// still be reported — the fix removes the false alarm, not the alarm.
	now := at12(t, "2026-07-10")
	var current, previous []domain.FinancialEntry
	for d := 1; d <= 10; d++ {
		current = append(current, sale(t, fmt.Sprintf("2026-07-%02d", d), 5000))
		previous = append(previous, sale(t, fmt.Sprintf("2026-06-%02d", d), 10000))
	}

	got := Build(Input{
		Month:           "2026-07",
		Entries:         current,
		PreviousEntries: previous,
		Summaries:       []*pkgfinance.MonthlySummary{nil, summary(100000, 0), summary(50000, 0)},
		Now:             now,
	})

	if got.Trends.Receita.Direction != TrendDown || got.Trends.Receita.Change != -50 {
		t.Errorf("Receita trend = %+v, want down 50%%", got.Trends.Receita)
	}
	var dropped *Insight
	for i, m := range got.Health.Messages {
		if m.Type == InsightRevenueDrop {
			dropped = &got.Health.Messages[i]
		}
	}
	if dropped == nil {
		t.Fatalf("expected a revenue-drop insight, got %+v", got.Health.Messages)
	}
	if want := "50% abaixo do mês passado (até o dia 10)"; dropped.Description != want {
		t.Errorf("description = %q, want %q — the window has to be stated", dropped.Description, want)
	}
}
