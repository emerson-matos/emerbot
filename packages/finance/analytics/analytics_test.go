package analytics

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
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

// clock places a "YYYY-MM-DD" instant inside a "YYYY-MM" month, the way Build
// does — the single place the analysis decides what has finished and what is
// still ahead.
func clock(t *testing.T, month, date string) monthClock {
	t.Helper()
	return newMonthClock(month, at12(t, date))
}

func sale(t *testing.T, date string, amount int64) domain.FinancialEntry {
	t.Helper()
	return domain.FinancialEntry{
		TransactionDate: day(t, date),
		Amount:          amount,
		Type:            domain.EntryTypeIncome,
		Category:        "venda_balcao",
		Origin:          domain.OriginVenda,
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

// ratesFor builds the per-weekday rates a projection is priced from, Sunday
// first, so a test can state what a day of the week is worth without going
// through a window of fixture sales. sample is set past a week so the rates
// read as an ordinary, fully-backed projection.
func ratesFor(avgs ...int64) dailyRates {
	rates := dailyRates{sample: daysInWeek}
	copy(rates.avg[:], avgs)
	return rates
}

// fridayOnly is the shape most projection tests want: an ordinary Friday worth
// R$1.000,00 and nothing else in the week.
var fridayOnly = ratesFor(0, 0, 0, 0, 0, 100000, 0)

func at12(t *testing.T, date string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("parse now %q: %v", date, err)
	}
	return parsed.Add(12 * time.Hour)
}

// summary builds a month where every inflow is a paid sale — the common case
// the analytics assertions are written against. Tests that need faturamento and
// entradas de caixa to diverge set the fields directly.
func summary(income, expense int64) *pkgfinance.MonthlySummary {
	return &pkgfinance.MonthlySummary{
		TotalRevenue:    income,
		TotalCashIn:     income,
		TotalExpectedIn: income,
		TotalExpense:    expense,
		ExpectedBalance: income - expense,
	}
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
	// over two Mondays, not three sales. With Gaussian weighting and both
	// Mondays in the most recent weeks, the weighted average reflects both.
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-06", 10000),
		sale(t, "2026-07-06", 20000),
		sale(t, "2026-07-13", 30000),
		expense(t, "2026-07-06", "aluguel", 90000),
	}
	// Window: June 19 – July 14 (through=14 on July 15, end=first+13=July 14,
	// start=end-55=June 19).
	from := day(t, "2026-06-19")
	to := day(t, "2026-07-14")

	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-15")) // a Wednesday

	monday := stats[1]
	if monday.Label != "Seg" {
		t.Fatalf("index 1 should be Monday, got %q", monday.Label)
	}
	if monday.Count != 2 {
		t.Errorf("Count = %d, want 2 distinct Mondays", monday.Count)
	}
	// July 6 (offset=1, w≈0.882) * 30000 + July 13 (offset=0, w=1.0) * 30000
	// avg = 56467 / 1.882 ≈ 30000.
	// Both Mondays happen to have the same total per day, so the weighted avg
	// is pulled slightly toward the more recent one but stays at 30000.
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
	h := buildHighlights(nil, nil)
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

	h := buildHighlights(entries, entries)

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

// TestALoanIsNotASalesDayButIsStillCash guards the "empréstimo não é
// faturamento" rule where it is easiest to get wrong: a loan must not
// manufacture a best *sales* day or inflate the weekday averages, but it is
// real money and must still count toward the day's *balance* highlight. The
// two highlights read two different accumulators for exactly this reason.
//
// The loan carries a sales category on purpose — the origin decides, not the
// category. That is the whole point of the field: the old rule looked at
// "outros_receitas" and so a loan filed anywhere else counted as revenue.
func TestALoanIsNotASalesDayButIsStillCash(t *testing.T) {
	loan := domain.FinancialEntry{
		TransactionDate: day(t, "2026-07-10"),
		Amount:          10000000,
		Type:            domain.EntryTypeIncome,
		Category:        "venda_balcao",
		Origin:          domain.OriginEmprestimo,
	}
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 50000),
		loan,
	}

	h := buildHighlights(entries, entries)
	if h.BestIncome.Date != "2026-07-01" || h.BestIncome.Amount != 50000 {
		t.Errorf("BestIncome = %+v, want the 2026-07-01 sale, not the loan", h.BestIncome)
	}
	if h.BestBalance.Date != "2026-07-10" || h.BestBalance.Amount != 10000000 {
		t.Errorf("BestBalance = %+v, want the loan's day to still show the cash", h.BestBalance)
	}

	// The loan is not revenue, so it must not appear in the weekday averages.
	// Wednesday (July 1) has only the sale.
	from := day(t, "2026-06-15")
	to := day(t, "2026-07-14")
	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-15"))

	wednesday := stats[3] // July 1 is a Wednesday
	if wednesday.Avg != 50000 {
		t.Errorf("Wednesday avg = %d, want 50000 (sale only, loan excluded)", wednesday.Avg)
	}
	// Thursday (July 10) has only the loan, which is not revenue — must be empty.
	thursday := stats[4]
	if thursday.Count != 0 || thursday.Avg != 0 {
		t.Errorf("Thursday = %+v, want nothing — the loan is not revenue", thursday)
	}
}

// TestTrendsSeparateFaturamentoFromResultado is the bug the reviewer caught:
// monthTotals used to hold one income figure, and balance was built from it, so
// narrowing income to sales would have turned Resultado into
// "sales minus every expense" — reporting a loss for a month an aporte covered.
func TestTrendsSeparateFaturamentoFromResultado(t *testing.T) {
	now := at12(t, "2026-07-31")
	aporte := func(date string, amount int64) domain.FinancialEntry {
		return domain.FinancialEntry{
			TransactionDate: day(t, date),
			Amount:          amount,
			Type:            domain.EntryTypeIncome,
			Category:        "outros_receitas",
			Origin:          domain.OriginAporteSocio,
		}
	}

	// July sold less than June, but an aporte more than covered the expenses.
	july := []domain.FinancialEntry{sale(t, "2026-07-05", 40000), aporte("2026-07-06", 200000), expense(t, "2026-07-07", "aluguel", 60000)}
	june := []domain.FinancialEntry{sale(t, "2026-06-05", 100000), expense(t, "2026-06-07", "aluguel", 60000)}

	c := buildComparison(newMonthClock("2026-07", now), july, june, july, june)
	trends := buildTrends(c)

	// Faturamento fell 60% — the aporte does not hide that.
	if trends.Faturamento.Direction != TrendDown {
		t.Errorf("Faturamento direction = %q, want down: sales went 100000 -> 40000", trends.Faturamento.Direction)
	}
	if trends.Faturamento.Current != 40000 {
		t.Errorf("Faturamento current = %d, want 40000 (sales only)", trends.Faturamento.Current)
	}
	// Resultado rose, because the aporte is real money that really did land.
	if trends.Resultado.Current != 180000 {
		t.Errorf("Resultado current = %d, want 180000 (240000 in - 60000 out)", trends.Resultado.Current)
	}
	if trends.Resultado.Direction != TrendUp {
		t.Errorf("Resultado direction = %q, want up: 40000 -> 180000", trends.Resultado.Direction)
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
	// 10 July; July has 31 days.
	sum := pkgfinance.MonthlySummary{TotalRevenue: 40000, TotalExpectedIn: 50000, TotalExpense: 30000}

	t.Run("without a goal", func(t *testing.T) {
		got := goalProgress(sum, nil, clock(t, "2026-07", "2026-07-10"), 40000)
		if got.RevenueTarget != 0 || got.RevenuePct != 0 {
			t.Errorf("targets = %+v, want zeroes with no goal set", got)
		}
		if got.RevenueActual != 40000 {
			t.Errorf("RevenueActual = %d, want the faturamento total", got.RevenueActual)
		}
		// Today counts as a day still to trade: the 10th through the 31st is
		// 22 days, not 21.
		if got.DaysRemaining != 22 || got.DaysTotal != 31 {
			t.Errorf("days = %d/%d, want 22/31", got.DaysRemaining, got.DaysTotal)
		}
	})

	t.Run("percentages cap at 100", func(t *testing.T) {
		goal := &domain.Goal{RevenueTarget: 20000, ExpenseTarget: 60000}
		got := goalProgress(sum, goal, clock(t, "2026-07", "2026-07-10"), 40000)
		if got.RevenuePct != 100 {
			t.Errorf("IncomePct = %d, want it capped at 100", got.RevenuePct)
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
	if got[1].Month != "2026-06" || got[1].Revenue != 0 {
		t.Errorf("middle month = %+v, want an empty 2026-06 in place", got[1])
	}
	if got[0].RevenueTarget != nil {
		t.Errorf("IncomeTarget = %v, want nil for a month with no goal", *got[0].RevenueTarget)
	}
	if got[2].RevenueTarget == nil || *got[2].RevenueTarget != 5000 {
		t.Errorf("last IncomeTarget = %v, want 5000", got[2].RevenueTarget)
	}
	if got[2].Label != "jul. de 2026" {
		t.Errorf("Label = %q, want %q", got[2].Label, "jul. de 2026")
	}
}

func TestCashPosition(t *testing.T) {
	now := at12(t, "2026-07-10")
	// No trading history, so the days ahead are credited with nothing and the
	// curve is exactly the booked one — the shape these cases are about.
	var noRates dailyRates

	t.Run("no projection", func(t *testing.T) {
		got := buildCashPosition(nil, noRates, now)
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
		got := buildCashPosition(points, noRates, now)

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
		if got := buildCashPosition(points, noRates, now); got.DaysUntilNegative != nil {
			t.Errorf("DaysUntilNegative = %v, want nil — the crossing has already happened", *got.DaysUntilNegative)
		}
	})
}

func TestWeekComparison(t *testing.T) {
	// Wednesday 2026-07-15. This week starts Monday the 13th; last week ran
	// Monday the 6th to Sunday the 12th. Two days of this week have finished
	// (Mon 13, Tue 14), so the pace covers Mon–Tue on both sides.
	now := at12(t, "2026-07-15")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-13", 10000),
		sale(t, "2026-07-15", 10000),
		sale(t, "2026-07-16", 99999), // tomorrow — must not count
		sale(t, "2026-07-06", 5000),
		sale(t, "2026-07-08", 5000),
		sale(t, "2026-07-12", 20000), // last Sunday: in Previous, outside the pace window
	}

	got := buildWeekComparison(entries, now, 500000)

	if got.Current != 20000 {
		t.Errorf("Current = %d, want 20000", got.Current)
	}
	if got.Previous != 30000 {
		t.Errorf("Previous = %d, want the whole of last week (30000)", got.Previous)
	}
	// Today is in neither side of the pace: Monday the 13th here, Monday the
	// 6th there. Wednesday's own takings are still being made.
	if got.Pace.Days != 2 {
		t.Errorf("Pace.Days = %d, want the two finished days of this week", got.Pace.Days)
	}
	if got.Pace.Current != 10000 {
		t.Errorf("Pace.Current = %d, want Mon–Tue this week (10000)", got.Pace.Current)
	}
	if got.Pace.Previous != 5000 {
		t.Errorf("Pace.Previous = %d, want Mon–Tue last week (5000)", got.Pace.Previous)
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

	got := buildWeekComparison(entries, now, 0)

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
	// Two closed months, so the comparison is whole-against-whole. Both insights
	// read the revenue side: they are performance readings, so borrowed money
	// must not soften either one.
	compared := comparison{
		current:  monthTotals{revenue: 80000, income: 80000, expense: 60000, balance: 20000},
		previous: monthTotals{revenue: 100000, income: 100000, expense: 40000, balance: 60000},
		clock:    clock(t, "2026-06", "2026-07-10"),
	}

	health := buildHealth(nil, compared, WeekComparison{}, Projection{})

	byType := map[InsightType]Insight{}
	for _, m := range health.Messages {
		byType[m.Type] = m
	}

	drop, ok := byType[InsightRevenueDrop]
	if !ok {
		t.Fatalf("expected an income-drop insight, got %+v", health.Messages)
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
	// 10 of 30 days gone, R$1.000,00 of a R$10.000,00 target: R$450/day still
	// needed across the 20 days left, and a projection that misses.
	projection := Projection{
		Actual: 100000, Projected: 300000, Target: 1000000,
		Gap: 700000, DaysRemaining: 20, NeededPerDay: 45000,
	}
	// Mid-month, with a day already behind us, so the pacing insight is the
	// only thing this test is looking at.
	health := buildHealth(nil, comparison{clock: clock(t, "2026-07", "2026-07-11")}, WeekComparison{}, projection)

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

	positive, total := countDays(entries, 31)

	if positive != 1 || total != 2 {
		t.Errorf("countDays = %d of %d, want 1 of 2", positive, total)
	}
}

func TestRecommendationsWeeklyPaceMatrix(t *testing.T) {
	// 10 of 30 days gone against a R$10.000,00 target; whether the projection
	// reaches it is what decides "on track" — the same verdict the dashboard
	// card and the health insight read.
	onTrack := Projection{
		Actual: 500000, Remaining: 600000, Projected: 1100000, Target: 1000000,
		OnTrack: true, DaysRemaining: 20, NeededPerDay: 25000,
	}
	behind := Projection{
		Actual: 100000, Remaining: 200000, Projected: 300000, Target: 1000000,
		Gap: 700000, DaysRemaining: 20, NeededPerDay: 45000,
	}

	improved := WeekComparison{Pace: WeekPace{Current: 12000, Previous: 10000, Days: 3}}
	declined := WeekComparison{Pace: WeekPace{Current: 8000, Previous: 10000, Days: 3}}
	stable := WeekComparison{Pace: WeekPace{Current: 10000, Previous: 10000, Days: 3}}

	tests := []struct {
		name       string
		week       WeekComparison
		projection Projection
		want       string
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
			recs := buildRecommendations(tc.week, tc.projection, Trends{}, CashPosition{}, comparison{})
			if len(recs) == 0 {
				t.Fatal("expected a weekly recommendation")
			}
			if recs[0].Title != tc.want {
				t.Errorf("title = %q, want %q", recs[0].Title, tc.want)
			}
		})
	}
}

func TestRecommendationsNeedARealBaseline(t *testing.T) {
	// buildTrend reports a previous of zero as a flat 100% rise, because there
	// is no percentage over nothing. A month whose predecessor never traded
	// must not be told its expenses "cresceram 100%" against it.
	trends := Trends{
		Faturamento: buildTrend(0, 0),
		Despesa:     buildTrend(140000, 0),
	}
	if trends.Despesa.Change != 100 || trends.Despesa.Direction != TrendUp {
		t.Fatalf("precondition: buildTrend(140000, 0) = %+v, want the 100%% fallback", trends.Despesa)
	}

	recs := buildRecommendations(WeekComparison{}, Projection{}, trends, CashPosition{}, comparison{})

	if len(recs) != 0 {
		t.Errorf("recommendations = %+v, want none against a month with no expenses to compare to", recs)
	}
}

func TestProjectionIsTheOnlyPerDayAsk(t *testing.T) {
	// Sunday 2026-07-26: five days left in the month (Mon–Fri the 27th–31st).
	now := at12(t, "2026-07-26")
	rates := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	// The 26th through the 31st is six days, today included.
	goals := GoalProgress{
		RevenueTarget: 3600000, RevenueActual: 2777500,
		DaysTotal: 31, DaysRemaining: 6,
	}

	got := buildProjection(rates, goals, now, clock(t, "2026-07", "2026-07-26"), 0)

	if want := int64(600000); got.Remaining != want {
		t.Errorf("Remaining = %d, want six days at R$1.000,00 (%d)", got.Remaining, want)
	}
	if want := int64(3377500); got.Projected != want {
		t.Errorf("Projected = %d, want actual plus the days left (%d)", got.Projected, want)
	}
	if want := int64(222500); got.Gap != want {
		t.Errorf("Gap = %d, want what the projection still misses (%d)", got.Gap, want)
	}
	if got.OnTrack {
		t.Error("OnTrack = true, want false — the projection lands under the target")
	}
	// The one number the card, the insight and the recommendation all print:
	// what is still missing, spread over the days left.
	if want := int64(137083); got.NeededPerDay != want {
		t.Errorf("NeededPerDay = %d, want (target-actual)/daysRemaining (%d)", got.NeededPerDay, want)
	}
}

func TestProjectionCountsTodayAsADayStillToTrade(t *testing.T) {
	// 31 July, a Friday, and the shop is open. The projection used to start at
	// *tomorrow*, so the last day of every month reported nothing left to
	// project and no per-day ask — the day was written off before it happened.
	now := at12(t, "2026-07-31")
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 900000, DaysTotal: 31, DaysRemaining: 1}

	got := buildProjection(fridayOnly, goals, now, clock(t, "2026-07", "2026-07-31"), 0)

	if want := int64(100000); got.Remaining != want {
		t.Errorf("Remaining = %d, want today's Friday average (%d)", got.Remaining, want)
	}
	if !got.OnTrack {
		t.Error("OnTrack = false, want true — an ordinary Friday closes the gap")
	}
	// Still R$1.000,00 short of the target, and one day to make it in.
	if want := int64(100000); got.NeededPerDay != want {
		t.Errorf("NeededPerDay = %d, want the whole shortfall on today (%d)", got.NeededPerDay, want)
	}
}

// A day that is already half traded must not be counted twice: what has sold
// today is in Actual, so only the rest of an ordinary day is still ahead.
func TestProjectionDoesNotCountTodayTwice(t *testing.T) {
	now := at12(t, "2026-07-31")
	goals := GoalProgress{RevenueActual: 900000, DaysTotal: 31, DaysRemaining: 1}

	got := buildProjection(fridayOnly, goals, now, clock(t, "2026-07", "2026-07-31"), 40000)

	if want := int64(60000); got.Remaining != want {
		t.Errorf("Remaining = %d, want the rest of today (%d)", got.Remaining, want)
	}

	// And a day that has already beaten its average has nothing left to add,
	// rather than a negative amount.
	beaten := buildProjection(fridayOnly, goals, now, clock(t, "2026-07", "2026-07-31"), 150000)
	if beaten.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0 once today has passed its average", beaten.Remaining)
	}
}

func TestProjectionHasNothingLeftForAClosedMonth(t *testing.T) {
	// Analysing July in August: the month is over, so there is no day left to
	// project into and no per-day ask to make of it.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 900000, DaysTotal: 31}

	got := buildProjection(fridayOnly, goals, at12(t, "2026-08-03"), clock(t, "2026-07", "2026-08-03"), 0)

	if got.Remaining != 0 {
		t.Errorf("Remaining = %d, want nothing left to project", got.Remaining)
	}
	if got.OnTrack {
		t.Error("OnTrack = true, want false — the month closed under its target")
	}
	if want := int64(100000); got.Gap != want {
		t.Errorf("Gap = %d, want %d", got.Gap, want)
	}
	// No days left to spread the shortfall over, so there is no ask to make.
	if got.NeededPerDay != 0 {
		t.Errorf("NeededPerDay = %d, want 0 with no days left", got.NeededPerDay)
	}
	// And nothing was estimated: Projected is July's own faturamento. Reading
	// the basis off the rates called that "janela" — an eight-week estimate,
	// stamped on the one figure in this struct that is not an estimate — and the
	// card captioned a realised total as a forecast.
	if got.Basis != ProjectionClosed {
		t.Errorf("Basis = %q, want %q for a month that has already ended", got.Basis, ProjectionClosed)
	}
	// It says so even when no window was fetched, so a caller that skips the
	// read for a closed month cannot turn the label into "sem_base".
	unfetched := buildProjection(dailyRates{}, goals, at12(t, "2026-08-03"), clock(t, "2026-07", "2026-08-03"), 0)
	if unfetched.Basis != ProjectionClosed {
		t.Errorf("Basis = %q without a window, want %q", unfetched.Basis, ProjectionClosed)
	}
}

func TestProjectionWithoutAGoalHasNothingToPace(t *testing.T) {
	now := at12(t, "2026-07-26")
	mondayOnly := ratesFor(0, 100000, 0, 0, 0, 0, 0)

	got := buildProjection(mondayOnly, GoalProgress{RevenueActual: 50000, DaysRemaining: 6}, now, clock(t, "2026-07", "2026-07-26"), 0)

	if got.Pacing() {
		t.Error("Pacing = true, want false with no target")
	}
	if got.NeededPerDay != 0 || got.Gap != 0 || got.OnTrack {
		t.Errorf("got %+v, want no verdict without a target", got)
	}
	// The projection itself still stands: Monday the 27th is the only day left
	// with an average.
	if want := int64(150000); got.Projected != want {
		t.Errorf("Projected = %d, want %d", got.Projected, want)
	}
}

// The bug this window exists for. On 3 August the pharmacy had traded one
// Saturday and one Sunday; every other day of the week had never been seen
// *this month*, so the twenty-one weekdays still to come were each priced at
// zero and the month projected a quarter of its goal. A day of the week is a
// property of the pharmacy, not of the calendar month.
func TestProjectionRatesLearnTheWeekFromTheTrailingWindow(t *testing.T) {
	// July traded every weekday; August has only had its opening weekend.
	window := []domain.FinancialEntry{
		sale(t, "2026-07-06", 150000), // Monday
		sale(t, "2026-07-07", 150000), // Tuesday
		sale(t, "2026-07-08", 150000), // Wednesday
		sale(t, "2026-07-09", 150000), // Thursday
		sale(t, "2026-07-10", 150000), // Friday
		sale(t, "2026-07-11", 100000), // Saturday
		sale(t, "2026-07-12", 50000),  // Sunday
		sale(t, "2026-08-01", 120000), // Saturday
		sale(t, "2026-08-02", 60000),  // Sunday
	}

	from, to := clock(t, "2026-08", "2026-08-03").projectionWindow()
	rates := projectionRates(window, from, to)

	for day := 1; day <= 5; day++ {
		if rates.avg[day] == 0 {
			t.Errorf("weekday %d priced at 0, want July's average — this is the start-of-month blank", day)
		}
	}
	// Saturday appears in both months, so its rate is the average of the two.
	if want := int64(110000); rates.avg[6] != want {
		t.Errorf("Saturday = %d, want both Saturdays averaged (%d)", rates.avg[6], want)
	}
	if rates.basis() != ProjectionFromWindow {
		t.Errorf("basis = %q, want an ordinary projection", rates.basis())
	}
}

func TestProjectionWindowEndsOnTheLastFinishedDay(t *testing.T) {
	tests := []struct {
		name             string
		month, now       string
		wantFrom, wantTo string
	}{
		// Eight whole weeks back from yesterday.
		{"mid-month", "2026-08", "2026-08-03", "2026-06-08", "2026-08-02"},
		// Nothing of the month has finished, so the window ends the day before
		// it began — and the projection is fully priced from the moment the
		// month opens.
		{"the first day", "2026-08", "2026-08-01", "2026-06-06", "2026-07-31"},
		// A closed month is priced from its own last day, never from today:
		// July opened in August must not be projected out of August's sales.
		{"a closed month", "2026-07", "2026-08-03", "2026-06-06", "2026-07-31"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			from, to := clock(t, tc.month, tc.now).projectionWindow()
			if from.String() != tc.wantFrom || to.String() != tc.wantTo {
				t.Errorf("window = %s..%s, want %s..%s", from, to, tc.wantFrom, tc.wantTo)
			}
			// Whole weeks, so no day of the week gets more chances than another.
			days := int(to.Time().Sub(from.Time()).Hours()/24) + 1
			if days != projectionWindowWeeks*daysInWeek {
				t.Errorf("window = %d days, want %d", days, projectionWindowWeeks*daysInWeek)
			}
		})
	}
}

func TestProjectionRatesIgnoreWhatFallsOutsideTheWindow(t *testing.T) {
	from, to := clock(t, "2026-08", "2026-08-03").projectionWindow() // 08/06..02/08
	window := []domain.FinancialEntry{
		sale(t, "2026-06-01", 900000), // a Monday one week too old
		sale(t, "2026-06-15", 100000), // a Monday inside
		sale(t, "2026-08-03", 900000), // today, still being traded
	}

	rates := projectionRates(window, from, to)

	if want := int64(100000); rates.avg[1] != want {
		t.Errorf("Monday = %d, want only the Monday inside the window (%d)", rates.avg[1], want)
	}
	if rates.sample != 1 {
		t.Errorf("sample = %d, want the single day inside the window", rates.sample)
	}
}

func TestProjectionRatesKeepADayTheShopNeverOpensAtZero(t *testing.T) {
	// Eight weeks of Mondays and Sundays, and never a Wednesday: the pharmacy
	// does not open on Wednesdays, so projecting takings for one would overstate
	// every month.
	var window []domain.FinancialEntry
	for _, d := range []string{"06-15", "06-22", "06-29", "07-06", "07-13", "07-20", "07-27"} {
		window = append(window, sale(t, "2026-"+d, 100000))
	}
	for _, d := range []string{"06-14", "06-21", "06-28", "07-05", "07-12", "07-19", "07-26"} {
		window = append(window, sale(t, "2026-"+d, 60000))
	}

	from, to := clock(t, "2026-08", "2026-08-03").projectionWindow()
	rates := projectionRates(window, from, to)

	if rates.avg[3] != 0 {
		t.Errorf("Wednesday = %d, want 0 — the shop never opened on one in eight weeks", rates.avg[3])
	}
	if rates.avg[1] != 100000 {
		t.Errorf("Monday = %d, want 100000", rates.avg[1])
	}
}

// A thin window under-projects rather than over-projects, and says so through
// the basis. An unobserved weekday used to take the overall daily average
// whenever fewer than seven days had traded — which could not tell a new user
// from a shop that opens twice a week, and put the overstating side of a
// sixfold step in front of whoever had the least data.
func TestProjectionRatesDoNotInventTakingsForAThinWindow(t *testing.T) {
	window := []domain.FinancialEntry{
		sale(t, "2026-08-01", 100000), // Saturday
		sale(t, "2026-08-02", 200000), // Sunday
	}

	from, to := clock(t, "2026-08", "2026-08-03").projectionWindow()
	rates := projectionRates(window, from, to)

	if rates.avg[3] != 0 {
		t.Errorf("an unobserved Wednesday = %d, want 0 rather than a spread average", rates.avg[3])
	}
	// The days that *were* observed keep their own figures.
	if rates.avg[6] != 100000 || rates.avg[0] != 200000 {
		t.Errorf("observed weekdays = %d/%d, want their own averages", rates.avg[6], rates.avg[0])
	}
	if rates.basis() != ProjectionPartial {
		t.Errorf("basis = %q, want %q", rates.basis(), ProjectionPartial)
	}

	if empty := projectionRates(nil, from, to); empty.basis() != ProjectionNoBasis {
		t.Errorf("basis = %q, want %q with nothing in the window", empty.basis(), ProjectionNoBasis)
	}
}

// The step that removing the spread got rid of. A pharmacy that only opens on
// Saturdays must project the same whether six or seven of the window's
// Saturdays are on record — one more day of data must not move the month.
func TestProjectionRatesDoNotLurchOnOneMoreTradingDay(t *testing.T) {
	saturdays := []string{"2026-06-13", "2026-06-20", "2026-06-27", "2026-07-04", "2026-07-11", "2026-07-18"}
	var six []domain.FinancialEntry
	for _, d := range saturdays {
		six = append(six, sale(t, d, 1000000))
	}
	seven := append(append([]domain.FinancialEntry{}, six...), sale(t, "2026-07-25", 1000000))

	from, to := clock(t, "2026-08", "2026-08-03").projectionWindow()
	withSix := projectionRates(six, from, to)
	withSeven := projectionRates(seven, from, to)

	for d := 0; d < daysInWeek; d++ {
		if d == 6 {
			continue
		}
		if withSix.avg[d] != 0 {
			t.Errorf("weekday %d = %d on six Saturdays, want 0 — the shop does not open then", d, withSix.avg[d])
		}
	}
	if withSix.avg[6] != withSeven.avg[6] {
		t.Errorf("Saturday = %d then %d; one more Saturday must not change its average",
			withSix.avg[6], withSeven.avg[6])
	}
}

// The weekday card is a factual reading of the analysed month — its empty state
// The weekday card now uses an 8-week trailing window. Entries outside the
// window are excluded; entries inside the window (even from other months) count
// with Gaussian weighting. This test verifies the window boundary is respected.
func TestWeekdayStatsUseTrailingWindow(t *testing.T) {
	entries := []domain.FinancialEntry{
		// A Wednesday in July, outside the 8-week window for August 3:
		// window = June 26 – Aug 2. July 1 IS inside this window (June 26 <= July 1 <= Aug 2).
		sale(t, "2026-07-01", 500000),
		sale(t, "2026-08-01", 100000), // a Saturday, this month
	}
	// Window for August 3: from June 26 to Aug 2.
	from := day(t, "2026-06-26")
	to := day(t, "2026-08-02")

	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-08-03"))

	// July 1 (Wednesday) is inside the window, so Wednesday should have data.
	if stats[3].Count != 1 {
		t.Errorf("Wednesday Count = %d, want 1 (July 1 is inside the 8-week window)", stats[3].Count)
	}
	if stats[3].Avg != 500000 {
		t.Errorf("Wednesday Avg = %d, want 500000", stats[3].Avg)
	}
	// August 1 (Saturday) is inside the window.
	if stats[6].Avg != 100000 {
		t.Errorf("Saturday = %d, want August's own figure", stats[6].Avg)
	}
}

// An entry far outside the window must not leak into the card.
func TestWeekdayStatsExcludeEntriesOutsideWindow(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-05-01", 99999), // far outside the window for July
		sale(t, "2026-07-06", 10000), // a Monday, inside the window
	}
	// Window for July 15: from June 10 to July 14.
	from := day(t, "2026-06-10")
	to := day(t, "2026-07-14")

	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-15"))

	// Monday should only have the July 6 entry, not the May 1 one.
	monday := stats[1]
	if monday.Count != 1 {
		t.Errorf("Monday Count = %d, want 1 (May entry outside window)", monday.Count)
	}
	if monday.Avg != 10000 {
		t.Errorf("Monday Avg = %d, want 10000", monday.Avg)
	}
	// Wednesday (May 1) must not appear.
	wednesday := stats[3]
	if wednesday.Count != 0 || wednesday.Avg != 0 {
		t.Errorf("Wednesday = %+v, want nothing — May 1 is outside the window", wednesday)
	}
}

// Input.Now is already in the pharmacy's timezone, so the month it falls in must
// be read off its own calendar. Reading it in UTC disagreed with every other
// field on the clock for the last hours of each Brazilian evening.
func TestMonthClockReadsNowInItsOwnTimezone(t *testing.T) {
	saoPaulo := time.FixedZone("-03", -3*3600)
	lateOnTheLastOfJuly := time.Date(2026, 7, 31, 22, 0, 0, 0, saoPaulo) // already 1 August in UTC

	if c := newMonthClock("2026-07", lateOnTheLastOfJuly); !c.inProgress || c.today != 31 {
		t.Errorf("July = %+v, want the month still in progress on its last evening", c)
	}
	// And August has not started, so it must not be reported as in progress —
	// which used to hand it today=31, through=30 and a projection window running
	// thirty days into the future, plus the store read that goes with one.
	//
	// (A month that has not begun still lands in the same branch as a closed one,
	// which is why its window is meaningless rather than empty. That predates
	// this and costs nothing now: only a month in progress is ever priced, or
	// fetched for.)
	august := newMonthClock("2026-08", lateOnTheLastOfJuly)
	if august.inProgress {
		t.Errorf("August = %+v, want a month that has not begun to be treated as not in progress", august)
	}
}

func TestHealthScorePricesProblemsBySeverity(t *testing.T) {
	tests := []struct {
		name     string
		messages []Insight
		want     int
	}{
		{"a clean month", []Insight{{Severity: SeverityInfo}, {Severity: SeverityInfo}}, 100},
		{"one warning", []Insight{{Severity: SeverityInfo}, {Severity: SeverityWarning}}, 85},
		{"two warnings", []Insight{{Severity: SeverityWarning}, {Severity: SeverityWarning}}, 70},
		{"a critical costs more than a warning", []Insight{{Severity: SeverityCritical}}, 60},
		{"the floor is zero", []Insight{
			{Severity: SeverityCritical},
			{Severity: SeverityCritical},
			{Severity: SeverityCritical},
			{Severity: SeverityCritical},
		}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := healthScore(tc.messages); got != tc.want {
				t.Errorf("healthScore = %d, want %d", got, tc.want)
			}
		})
	}

	// Good news must not move the score: it used to be the share of insights
	// that were informational, so an extra cheerful line changed the number
	// without anything financial changing.
	warned := []Insight{{Severity: SeverityWarning}}
	if healthScore(warned) != healthScore(append(warned, Insight{Severity: SeverityInfo})) {
		t.Error("an added info insight moved the score")
	}
}

func TestRecommendationsWithoutAGoal(t *testing.T) {
	recs := buildRecommendations(WeekComparison{}, Projection{}, Trends{}, CashPosition{}, comparison{})
	if len(recs) != 0 {
		t.Errorf("recommendations = %+v, want none when there is nothing to pace against", recs)
	}
}

func TestRecommendationsFlagTrendsAndRunway(t *testing.T) {
	trends := Trends{
		Faturamento: MonthTrend{Current: 75000, Previous: 100000, Change: -25, Direction: TrendDown},
		Despesa:     MonthTrend{Current: 140000, Previous: 100000, Change: 40, Direction: TrendUp},
	}
	oneDay := 1
	// ExpectsReceipts: the crossing survives an ordinary day's takings being
	// counted, which is the only version of it worth an alert.
	cash := CashPosition{DaysUntilNegative: &oneDay, ExpectsReceipts: true}

	// A closed month, so the messages carry no "até o dia N" caveat.
	recs := buildRecommendations(WeekComparison{}, Projection{}, trends, cash, comparison{clock: clock(t, "2026-06", "2026-07-10")})

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
	want2 := "Mesmo contando o recebimento de um dia normal, o saldo fica negativo em 1 dia. Reduza despesas ou antecipe recebimentos."
	if recs[2].Message != want2 {
		t.Errorf("runway message = %q, want %q", recs[2].Message, want2)
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

func TestMonthRange(t *testing.T) {
	got := MonthRange("2026-01", 3)
	want := []string{"2025-11", "2025-12", "2026-01"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MonthRange = %v, want %v", got, want)
		}
	}

	// An unparseable month yields just that month, so a caller still gets a
	// usable window instead of an empty one. Month *bounds* are the domain's
	// job now (see domain.ParseMonth) — this package no longer re-derives them.
	if got := MonthRange("julho", 3); len(got) != 1 || got[0] != "julho" {
		t.Fatalf("MonthRange(%q) = %v, want the month back unchanged", "julho", got)
	}
}

func TestBuildProducesAWholeAnalysis(t *testing.T) {
	now := at12(t, "2026-07-15")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 200000),
		sale(t, "2026-07-13", 100000),
		sale(t, "2026-07-14", 150000),
		sale(t, "2026-07-15", 99999), // today, still being traded — in no comparison
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
		// The fixtures are all same-day settled sales, so the transaction-basis
		// read returns the same rows. A test that needs the two bases to differ
		// passes different slices — see TestCrediarioCountsInTheMonthItWasSold.
		RevenueEntries:         entries,
		PreviousRevenueEntries: previous,
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
	if got.KPIs.Faturamento != 450000 || got.KPIs.Despesa != 300000 || got.KPIs.Resultado != 150000 {
		t.Errorf("KPIs = %+v, want the analysed month's summary", got.KPIs)
	}
	// The 15th through the 31st, today included.
	if got.Period.DaysRemaining != 17 || got.Period.ThroughDay != 14 || !got.Period.InProgress {
		t.Errorf("Period = %+v, want 17 days left and complete data through the 14th", got.Period)
	}
	// Last month over the same finished days — the like-for-like side of the
	// faturamento trend, and the only place this figure lives now.
	if got.Trends.Faturamento.Previous != 400000 {
		t.Errorf("Trends.Faturamento.Previous = %d, want last month through the 14th", got.Trends.Faturamento.Previous)
	}
	if got.Goals.RevenueActual != 450000 {
		t.Errorf("Goals.RevenueActual = %d, want the faturamento total", got.Goals.RevenueActual)
	}
	// July is genuinely *ahead* over the days that have finished: 450.000 by
	// the 14th against June's 400.000 by its 14th. Comparing against June's
	// closed 1.300.000 would call that a collapse, which is the whole point
	// of measuring both months over the same finished days.
	if got.Trends.Faturamento.Direction != TrendUp {
		t.Errorf("Receita trend = %+v, want up", got.Trends.Faturamento)
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

// The analysis page shows the per-day ask three times — in the health
// insight, in the recommendation and on the projection card — and the bot
// reads it back a fourth. They were computed separately and disagreed: the
// card divided the shortfall left *after* its own projection, everyone else
// divided the shortfall from real income, and the page told the user two
// different daily targets at once.
func TestBuildQuotesOnePerDayAskEverywhere(t *testing.T) {
	now := at12(t, "2026-07-15")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 200000),
		sale(t, "2026-07-14", 100000),
	}

	got := Build(Input{
		Month:          "2026-07",
		Entries:        entries,
		RevenueEntries: entries,
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(300000, 0)},
		Goals:          []*domain.Goal{nil, nil, {RevenueTarget: 2000000}},
		Now:            now,
	})

	if got.Projection.NeededPerDay <= 0 {
		t.Fatalf("Projection = %+v, want a per-day ask", got.Projection)
	}
	asked := formatBRL(got.Projection.NeededPerDay)

	var insight string
	for _, m := range got.Health.Messages {
		if m.Type == InsightGoalBehind {
			insight = m.Description
		}
	}
	if !strings.Contains(insight, asked) {
		t.Errorf("health insight = %q, want it to quote %s", insight, asked)
	}
	if len(got.Recommendations) == 0 || !strings.Contains(got.Recommendations[0].Message, asked) {
		t.Errorf("recommendation = %+v, want it to quote %s", got.Recommendations, asked)
	}
	if want := reais(got.Projection.NeededPerDay); got.ToolPayload()["necessario_por_dia_para_bater_a_meta"] != want {
		t.Errorf("tool payload per-day ask = %v, want %v", got.ToolPayload()["necessario_por_dia_para_bater_a_meta"], want)
	}
	// And one projection of the month, not one per consumer.
	if want := reais(got.Projection.Projected); got.ToolPayload()["projecao_do_mes"] != want {
		t.Errorf("projecao_do_mes = %v, want the shared projection (%v)", got.ToolPayload()["projecao_do_mes"], want)
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
		Period: Period{ThroughDay: 14, ComparableThroughDay: 14, DaysRemaining: 10, DaysTotal: 31, InProgress: true},
		Projection: Projection{
			Target: 1000000, Actual: 800000, DaysRemaining: 10, NeededPerDay: 20000,
		},
		Health: Health{
			Status: HealthAtencao,
			Messages: []Insight{
				{Severity: SeverityInfo, Title: "Resultado positivo", Description: "tudo certo"},
				{Severity: SeverityWarning, Title: "Receitas cairam", Description: "17% abaixo do mês passado"},
			},
		},
		Recommendations: []Recommendation{
			// recommendations[0] is always the weekly-pace one when a goal is
			// being paced, and its message is the per-day ask AheadLines prints
			// itself. The digest takes the next distinct point instead.
			{Title: "Ritmo estável mas não é suficiente", Message: "Precisa acelerar."},
			{Title: "Receita caiu", Message: "Aja rapidamente."},
		},
	}

	lines := analysis.DigestLines()

	want := []string{
		"Saúde do mês até ontem (dia 14): Atenção.",
		"Receitas cairam — 17% abaixo do mês passado.",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}

	// The one thing to do about it is the other half of the message, and it
	// leads with the per-day ask rather than with the diagnosis.
	ahead := analysis.AheadLines()
	wantAhead := []string{
		"Faltam R$ 2.000,00 para a meta: R$ 200,00/dia nos 10 dias que restam (hoje incluído).",
		"Receita caiu: Aja rapidamente.",
	}
	if len(ahead) != len(wantAhead) {
		t.Fatalf("ahead = %v, want %v", ahead, wantAhead)
	}
	for i := range wantAhead {
		if ahead[i] != wantAhead[i] {
			t.Errorf("ahead %d = %q, want %q", i, ahead[i], wantAhead[i])
		}
	}
}

// The month's first day has nothing behind it, and saying so is the whole of
// the retrospective half. This is the message the pharmacy actually received on
// 1 August: "saúde crítica", "fluxo negativo", "receita caiu 100%" — every one
// of them an artifact of comparing an untraded morning with a finished day.
func TestDigestSaysNothingAboutAMonthWithNoFinishedDay(t *testing.T) {
	analysis := Analysis{
		Period: Period{ThroughDay: 0, DaysRemaining: 31, DaysTotal: 31, InProgress: true},
		Health: Health{
			Status: HealthBoa,
			Messages: []Insight{
				{Type: InsightMonthStart, Severity: SeverityInfo, Title: "Mês começando", Description: "Ainda não há dia fechado para avaliar"},
			},
		},
		Projection: Projection{Target: 3100000, Actual: 0, DaysRemaining: 31, NeededPerDay: 100000},
		Recommendations: []Recommendation{
			{Title: "Ritmo estável mas não é suficiente", Message: "Precisa acelerar."},
			{Title: "Saldo fica negativo em breve", Message: "Reduza despesas."},
		},
	}

	lines := analysis.DigestLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "começando") {
		t.Errorf("DigestLines = %v, want one line saying the month is starting", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "%") {
			t.Errorf("digest line %q quotes a percentage with no finished day to measure", l)
		}
	}

	// What is still ahead is unaffected — it is the only actionable half of a
	// message that lands on the 1st.
	ahead := analysis.AheadLines()
	if len(ahead) != 2 || !strings.Contains(ahead[0], "R$ 1.000,00/dia") {
		t.Errorf("AheadLines = %v, want the per-day ask and the recommendation", ahead)
	}
	// The per-day ask is printed once, not twice: the weekly-pace
	// recommendation says the same thing and is dropped in its favour.
	if strings.Contains(ahead[1], "acelerar") {
		t.Errorf("AheadLines repeats the per-day ask: %v", ahead)
	}
}

func TestToolPayloadUsesReais(t *testing.T) {
	analysis := Build(Input{
		Month:          "2026-07",
		Entries:        []domain.FinancialEntry{sale(t, "2026-07-01", 123456)},
		RevenueEntries: []domain.FinancialEntry{sale(t, "2026-07-01", 123456)},
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(123456, 0)},
		Now:            at12(t, "2026-07-15"),
	})

	payload := analysis.ToolPayload()

	if payload["faturamento"] != 1234.56 {
		t.Errorf("faturamento = %v, want reais not centavos", payload["faturamento"])
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

	got := buildComparison(newMonthClock("2026-07", now), current, previous, current, previous)

	// The 9th, not the 10th: today is still being traded and is not a data
	// point on either side.
	if got.clock.through != 9 {
		t.Errorf("through = %d, want 9 — yesterday", got.clock.through)
	}
	if got.current.income != 40000 {
		t.Errorf("current income = %d, want only the finished days (40000)", got.current.income)
	}
	if got.previous.income != 50000 {
		t.Errorf("previous income = %d, want only the finished days (50000)", got.previous.income)
	}
}

func TestComparisonUsesWholeMonthsForAClosedMonth(t *testing.T) {
	// Analysing June while it is July: June is over, so cutting it at the
	// 10th would be arbitrary.
	now := at12(t, "2026-07-10")
	june := []domain.FinancialEntry{sale(t, "2026-06-05", 50000), sale(t, "2026-06-25", 90000)}
	may := []domain.FinancialEntry{sale(t, "2026-05-28", 70000)}

	got := buildComparison(newMonthClock("2026-06", now), june, may, june, may)

	if got.clock.inProgress {
		t.Error("inProgress = true, want false — June is over")
	}
	if got.windowSuffix() != "" {
		t.Errorf("suffix = %q, want no window caveat on a closed month", got.windowSuffix())
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
	if got := totalsThroughDay([]domain.FinancialEntry{bill}, 31); got.expense != 30000 {
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
		Month:                  "2026-07",
		Entries:                current,
		PreviousEntries:        previous,
		RevenueEntries:         current,
		PreviousRevenueEntries: previous,
		Summaries: []*pkgfinance.MonthlySummary{
			nil, summary(300000, 0), summary(100000, 0),
		},
		Now: now,
	})

	if got.Trends.Faturamento.Direction != TrendStable || got.Trends.Faturamento.Change != 0 {
		t.Errorf("Receita trend = %+v, want stable at 0%% — same pace, same height of month",
			got.Trends.Faturamento)
	}
	if got.Period.ThroughDay != 9 || !got.Period.InProgress {
		t.Errorf("Period = %+v, want through day 9 (yesterday) so the UI can label the window",
			got.Period)
	}
	// And the false alarms that fell out of it are gone.
	for _, r := range got.Recommendations {
		if r.Title == "Receita caiu" {
			t.Errorf("still recommending %q for a month trading at last month's pace", r.Title)
		}
	}
	for _, m := range got.Health.Messages {
		if m.Type == InsightRevenueDrop {
			t.Errorf("still flagging an income drop: %+v", m)
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
		Month:                  "2026-07",
		Entries:                current,
		PreviousEntries:        previous,
		RevenueEntries:         current,
		PreviousRevenueEntries: previous,
		Summaries:              []*pkgfinance.MonthlySummary{nil, summary(100000, 0), summary(50000, 0)},
		Now:                    now,
	})

	if got.Trends.Faturamento.Direction != TrendDown || got.Trends.Faturamento.Change != -50 {
		t.Errorf("Receita trend = %+v, want down 50%%", got.Trends.Faturamento)
	}
	var dropped *Insight
	for i, m := range got.Health.Messages {
		if m.Type == InsightRevenueDrop {
			dropped = &got.Health.Messages[i]
		}
	}
	if dropped == nil {
		t.Fatalf("expected an income-drop insight, got %+v", got.Health.Messages)
	}
	// The 7th, not the 9th: the window closes on whole weeks, so days 8 and 9
	// wait for the second week to finish before they can be held against July.
	if want := "50% abaixo do mês passado (até o dia 7)"; dropped.Description != want {
		t.Errorf("description = %q, want %q — the window has to be stated", dropped.Description, want)
	}
}

// TestFirstDayOfTheMonthReportsNothingRetrospective is the bug this all comes
// from. On the morning of 1 August the digest went out saying the pharmacy's
// health was critical, its cash flow negative and its receita down 100% on the
// month before — a month whose only content so far was its own booked bills,
// weighed against a July that had traded for thirty-one days.
//
// Nothing here may quote a percentage, and the traffic light may not go red off
// a calendar of expenses nobody has paid yet.
func TestFirstDayOfTheMonthReportsNothingRetrospective(t *testing.T) {
	now := at12(t, "2026-08-01")

	// August's books on its first morning: the month's fixed costs are already
	// registered, and not a single sale has happened.
	august := []domain.FinancialEntry{
		expense(t, "2026-08-05", "aluguel", 300000),
		expense(t, "2026-08-10", "folha_pagamento", 900000),
	}
	// July traded every day, the 1st included.
	var july []domain.FinancialEntry
	for d := 1; d <= 31; d++ {
		july = append(july, sale(t, fmt.Sprintf("2026-07-%02d", d), 50000))
	}

	got := Build(Input{
		Month:                  "2026-08",
		Entries:                august,
		PreviousEntries:        july,
		RevenueEntries:         nil,
		PreviousRevenueEntries: july,
		Summaries: []*pkgfinance.MonthlySummary{
			summary(1400000, 1100000),
			summary(1550000, 1200000),
			// August as the stored summary sees it: every bill of the month
			// booked, nothing sold. This is the figure that used to drive the
			// traffic light straight to red.
			{Month: "2026-08", TotalExpense: 1200000, ExpectedBalance: -1200000},
		},
		Goals: []*domain.Goal{nil, nil, {RevenueTarget: 1550000}},
		Now:   now,
	})

	if got.Period.ThroughDay != 0 || got.Period.DaysRemaining != 31 {
		t.Errorf("Period = %+v, want no finished day and all 31 still to trade", got.Period)
	}
	if got.Trends.Faturamento.Change != 0 || got.Trends.Faturamento.Direction == TrendDown {
		t.Errorf("Faturamento trend = %+v, want no movement — there is nothing behind us",
			got.Trends.Faturamento)
	}
	if got.Health.Status == HealthCritico {
		t.Errorf("health = %q, want the month not judged off its own booked bills", got.Health.Status)
	}
	for _, m := range got.Health.Messages {
		switch m.Type {
		case InsightRevenueDrop, InsightLowCashFlow, InsightExpenseGrowth:
			t.Errorf("insight %q fired on a month with no finished day: %+v", m.Type, m)
		}
	}
	for _, r := range got.Recommendations {
		if r.Title == "Receita caiu" {
			t.Errorf("still recommending %q before the shop has opened", r.Title)
		}
	}

	// And the half of the message that *is* honest on the 1st: the whole month
	// is still ahead, and the target is priced across all of it.
	if got.Projection.DaysRemaining != 31 {
		t.Errorf("Projection.DaysRemaining = %d, want the whole month still to trade", got.Projection.DaysRemaining)
	}
	if want := int64(50000); got.Projection.NeededPerDay != want {
		t.Errorf("NeededPerDay = %d, want the target spread over all 31 days (%d)", got.Projection.NeededPerDay, want)
	}

	lines := got.DigestLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "começando") {
		t.Errorf("DigestLines = %v, want a single line saying the month is starting", lines)
	}
	if len(got.AheadLines()) == 0 {
		t.Error("AheadLines is empty — the 1st has nothing but what lies ahead")
	}
}

// A month-over-month percentage must never leave the package without saying
// which days it covers. The model that rewrites the digest drops any caveat it
// is not handed, and "100% abaixo do mês passado (até o dia 1)" became "queda
// de 100% em relação ao mês passado" on its way to a WhatsApp.
func TestMonthOverMonthMessagesAlwaysNameTheirWindow(t *testing.T) {
	trends := Trends{
		Faturamento: MonthTrend{Current: 75000, Previous: 100000, Change: -25, Direction: TrendDown},
		Despesa:     MonthTrend{Current: 140000, Previous: 100000, Change: 40, Direction: TrendUp},
	}
	// The 14th: thirteen days have closed, of which the first seven are the
	// comparable window.
	window := comparison{clock: clock(t, "2026-07", "2026-07-14")}

	for _, r := range buildRecommendations(WeekComparison{}, Projection{}, trends, CashPosition{}, window) {
		if !strings.Contains(r.Message, "até o dia 7") {
			t.Errorf("recommendation %q does not name the window it measured: %q", r.Title, r.Message)
		}
	}
}

// The bug the comparison window exists for. On 3 August 2026 the pharmacy had
// traded its opening weekend at exactly the rate every July weekend had brought
// — it was doing precisely as well as the month before. The comparison ran the
// 1st and 2nd, a Saturday and a Sunday, against the 1st and 2nd of July, a
// Wednesday and a Thursday, and reported a 53% collapse with a red alert.
func TestOpeningWeekDoesNotCompareMismatchedWeekdays(t *testing.T) {
	var entries []domain.FinancialEntry
	// July worked every day: R$1.500,00 on weekdays, R$700,00 at weekends.
	for d := day(t, "2026-07-01").Time(); !d.After(day(t, "2026-07-31").Time()); d = d.AddDate(0, 0, 1) {
		amount := int64(150000)
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			amount = 70000
		}
		entries = append(entries, sale(t, d.Format("2006-01-02"), amount))
	}
	// August's Saturday and Sunday, at July's own weekend rate.
	august := []domain.FinancialEntry{sale(t, "2026-08-01", 70000), sale(t, "2026-08-02", 70000)}

	got := Build(Input{
		Month:                  "2026-08",
		Entries:                august,
		PreviousEntries:        entries,
		RevenueEntries:         august,
		PreviousRevenueEntries: entries,
		Now:                    at12(t, "2026-08-03"),
	})

	if got.Period.ThroughDay != 2 {
		t.Errorf("ThroughDay = %d, want 2 — two days really have closed", got.Period.ThroughDay)
	}
	if got.Period.ComparableThroughDay != 0 {
		t.Errorf("ComparableThroughDay = %d, want 0 — no whole week has closed", got.Period.ComparableThroughDay)
	}
	if got.Trends.Faturamento.Direction != TrendStable || got.Trends.Faturamento.Change != 0 {
		t.Errorf("Faturamento trend = %+v, want no direction: a weekend against two weekdays is not a comparison",
			got.Trends.Faturamento)
	}
	for _, r := range got.Recommendations {
		if r.Title == "Receita caiu" {
			t.Errorf("recommendation %q fired on a pharmacy trading exactly as it did last month: %s", r.Title, r.Message)
		}
	}
	for _, m := range got.Health.Messages {
		if m.Type == InsightRevenueDrop || m.Type == InsightExpenseGrowth {
			t.Errorf("month-over-month insight %q fired in the opening week: %s", m.Type, m.Description)
		}
	}
}

func TestComparableWindowClosesOnWholeWeeks(t *testing.T) {
	tests := []struct {
		name       string
		month, now string
		want       int
	}{
		{"the first day", "2026-08", "2026-08-01", 0},
		{"mid opening week", "2026-08", "2026-08-03", 0},
		// The 8th: the 1st through the 7th have closed, one of every weekday.
		{"the last day of the opening week", "2026-08", "2026-08-07", 0},
		{"the first day with a whole week behind it", "2026-08", "2026-08-08", 7},
		{"the window holds until the next week closes", "2026-08", "2026-08-14", 7},
		{"two weeks", "2026-08", "2026-08-15", 14},
		{"a closed month is compared whole", "2026-07", "2026-08-03", 31},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := clock(t, tc.month, tc.now).comparableThrough(); got != tc.want {
				t.Errorf("comparableThrough = %d, want %d", got, tc.want)
			}
		})
	}
}

// The opening week loses the comparison, not the month. What has actually
// happened here still gets reported — and each line names the window it was
// measured over, which mid-month are two different windows.
func TestOpeningWeekStillReportsTheMonthItself(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-08-01", 70000),
		sale(t, "2026-08-02", 70000),
		expense(t, "2026-08-01", "aluguel", 30000),
	}
	compared := buildComparison(clock(t, "2026-08", "2026-08-03"), entries, nil, entries, nil)

	if compared.realized.balance != 110000 {
		t.Errorf("realized balance = %d, want both closed days (110000)", compared.realized.balance)
	}
	if compared.current.revenue != 0 {
		t.Errorf("comparison revenue = %d, want nothing — there is no comparable window yet", compared.current.revenue)
	}
	if compared.comparable() {
		t.Error("comparable = true, want false in the opening week")
	}

	health := buildHealth(entries, compared, WeekComparison{}, Projection{})
	var positive bool
	for _, m := range health.Messages {
		if m.Title == "Resultado positivo" {
			positive = true
			if want := "Entradas maiores que despesas (até o dia 2)"; m.Description != want {
				t.Errorf("description = %q, want %q", m.Description, want)
			}
		}
		if m.Type == InsightMonthStart {
			t.Error("month_start insight fired on the 3rd — two days have closed")
		}
	}
	if !positive {
		t.Errorf("expected the month's own result to still be reported, got %+v", health.Messages)
	}
}

// Mid-month the two windows diverge, and every line has to name its own. A
// result stated through the 13th beside a percentage measured through the 7th,
// both labelled "até o dia 13", is the misstatement this split exists to stop.
func TestTheTwoWindowsAreLabelledSeparately(t *testing.T) {
	c := comparison{clock: clock(t, "2026-07", "2026-07-14")}

	if want := " (até o dia 13)"; c.realizedSuffix() != want {
		t.Errorf("realizedSuffix = %q, want %q", c.realizedSuffix(), want)
	}
	if want := " (até o dia 7)"; c.windowSuffix() != want {
		t.Errorf("windowSuffix = %q, want %q", c.windowSuffix(), want)
	}
}

func TestDigestSaysWhyThereIsNoComparisonYet(t *testing.T) {
	analysis := Analysis{
		Period: Period{ThroughDay: 2, ComparableThroughDay: 0, DaysRemaining: 29, DaysTotal: 31, InProgress: true},
		Health: Health{Status: HealthBoa},
	}

	lines := analysis.DigestLines()
	last := lines[len(lines)-1]
	if want := "Comparação com o mês passado a partir do dia 8 — a primeira semana ainda não fechou."; last != want {
		t.Errorf("last line = %q, want %q", last, want)
	}
	if analysis.ToolPayload()["sem_semana_fechada_para_comparar"] != true {
		t.Error("the bot payload has to spell the missing comparison out; the model reads a 0 as a collapse")
	}

	// And on the 1st, where nothing has closed at all, the existing month-start
	// line already says it — no second sentence about the week.
	first := Analysis{Period: Period{ThroughDay: 0, DaysRemaining: 31, DaysTotal: 31, InProgress: true}}
	if got := first.DigestLines(); len(got) != 1 {
		t.Errorf("first-day digest = %v, want the single month-start line", got)
	}
}

// The runway used to be the month's whole bill list against none of its sales.
// Rent booked for the 5th sank the balance, and the alert fired at the start of
// every month on a pharmacy that was never in danger.
func TestRunwayCountsAnOrdinaryDaysReceipts(t *testing.T) {
	now := at12(t, "2026-08-03")
	// The booked curve: R$2.000,00 today, then R$1.500,00 of rent on the 5th.
	points := []pkgfinance.CashFlowPoint{
		{Date: "2026-08-03", RunningBalance: 200000},
		{Date: "2026-08-04", RunningBalance: 200000},
		{Date: "2026-08-05", ProjectedExpense: 350000, RunningBalance: -150000},
		{Date: "2026-08-06", RunningBalance: -150000},
	}
	// An ordinary day brings R$1.000,00 in.
	rates := dailyRates{sample: daysInWeek}
	for d := range rates.avg {
		rates.avg[d] = 100000
	}

	got := buildCashPosition(points, rates, now)

	if got.CurrentBalance != 200000 {
		t.Errorf("CurrentBalance = %d, want today's booked balance untouched (200000)", got.CurrentBalance)
	}
	if got.DaysUntilNegative != nil {
		t.Errorf("DaysUntilNegative = %d, want none: three days of ordinary takings cover the rent", *got.DaysUntilNegative)
	}
	if !got.ExpectsReceipts {
		t.Error("ExpectsReceipts = false, want true — the window had trading in it")
	}
	// R$3.000,00 booked to the 6th plus four days at R$1.000,00.
	if want := int64(250000); got.EndOfMonthProjection != want {
		t.Errorf("EndOfMonthProjection = %d, want %d", got.EndOfMonthProjection, want)
	}

	// A hole an ordinary day cannot fill is still reported.
	points[2] = pkgfinance.CashFlowPoint{Date: "2026-08-05", ProjectedExpense: 900000, RunningBalance: -700000}
	points[3] = pkgfinance.CashFlowPoint{Date: "2026-08-06", RunningBalance: -700000}
	deep := buildCashPosition(points, rates, now)
	if deep.DaysUntilNegative == nil || *deep.DaysUntilNegative != 2 {
		t.Errorf("DaysUntilNegative = %v, want 2 — this one really does go under", deep.DaysUntilNegative)
	}
}

// A day that has already booked more than its weekday usually brings is not
// credited twice, and never has takings subtracted from it.
func TestRunwayDoesNotCountAReceiptTwice(t *testing.T) {
	now := at12(t, "2026-08-03")
	points := []pkgfinance.CashFlowPoint{
		// Today has already taken R$800,00 of an ordinary R$1.000,00 day.
		{Date: "2026-08-03", ProjectedIncome: 80000, RunningBalance: 80000},
		// A crediário instalment lands on the 4th, well past an ordinary day.
		{Date: "2026-08-04", ProjectedIncome: 500000, RunningBalance: 580000},
	}
	rates := dailyRates{sample: daysInWeek}
	for d := range rates.avg {
		rates.avg[d] = 100000
	}

	got := buildCashPosition(points, rates, now)

	// R$5.800,00 booked, plus the R$200,00 left of today. The 4th adds nothing:
	// it has already received more than an ordinary Tuesday brings.
	if want := int64(600000); got.EndOfMonthProjection != want {
		t.Errorf("EndOfMonthProjection = %d, want %d", got.EndOfMonthProjection, want)
	}
}

// Without trading history the days ahead are priced at nothing, so the booked
// curve stands unaltered — and must not be presented as a balance running out.
func TestRunwayWithoutHistoryMakesNoClaim(t *testing.T) {
	points := []pkgfinance.CashFlowPoint{
		{Date: "2026-08-03", RunningBalance: 10000},
		{Date: "2026-08-05", RunningBalance: -50000},
	}
	got := buildCashPosition(points, dailyRates{}, at12(t, "2026-08-03"))

	if got.ExpectsReceipts {
		t.Error("ExpectsReceipts = true, want false with nothing in the window")
	}
	recs := buildRecommendations(WeekComparison{}, Projection{}, Trends{}, got, comparison{})
	for _, r := range recs {
		if r.Title == "Saldo fica negativo em breve" {
			t.Errorf("runway alert fired with no trading history to price the days ahead: %s", r.Message)
		}
	}
}

// The runway is about money landing, so it reads every inflow by its effective
// date — not faturamento by the day of the sale. A crediário sale made on a
// Monday and received on a Friday is a Friday receipt, and an aporte is a
// receipt even though it is not a sale.
func TestCashInRatesReadTheDayTheMoneyLands(t *testing.T) {
	due := day(t, "2026-07-10") // a Friday
	crediario := sale(t, "2026-07-06", 100000)
	crediario.DueDate = &due
	aporte := domain.FinancialEntry{
		TransactionDate: day(t, "2026-07-17"), // also a Friday
		Amount:          300000,
		Type:            domain.EntryTypeIncome,
		Category:        "outros_receitas",
		Origin:          domain.OriginAporteSocio,
	}
	entries := []domain.FinancialEntry{crediario, aporte, expense(t, "2026-07-10", "aluguel", 900000)}

	rates := cashInRates(entries, day(t, "2026-06-08"), day(t, "2026-08-02"))

	if rates.avg[int(time.Monday)] != 0 {
		t.Errorf("Monday = %d, want nothing — the sale was made then but paid later", rates.avg[int(time.Monday)])
	}
	if want := int64(200000); rates.avg[int(time.Friday)] != want {
		t.Errorf("Friday = %d, want both receipts averaged (%d)", rates.avg[int(time.Friday)], want)
	}
}

// --- Gaussian weighting tests ---

func TestGaussianWeightDistribution(t *testing.T) {
	// Offset 0 must be exactly 1.0.
	if w := gaussianWeight(0); w != 1.0 {
		t.Errorf("weight(0) = %f, want 1.0", w)
	}
	// Offset 2 (one σ): exp(-0.5) ≈ 0.607.
	if w := gaussianWeight(2); math.Abs(w-0.60653) > 0.001 {
		t.Errorf("weight(2) = %f, want ≈0.607", w)
	}
	// Offset 4 (two σ): exp(-2) ≈ 0.135.
	if w := gaussianWeight(4); math.Abs(w-0.13534) > 0.001 {
		t.Errorf("weight(4) = %f, want ≈0.135", w)
	}
	// Offset 7 (edge): very small but non-zero.
	if w := gaussianWeight(7); w > 0.05 {
		t.Errorf("weight(7) = %f, want < 0.05", w)
	}
	// Monotonically decreasing.
	for i := 1; i <= 7; i++ {
		if gaussianWeight(i) >= gaussianWeight(i-1) {
			t.Errorf("weight(%d) = %f >= weight(%d) = %f, want strictly decreasing",
				i, gaussianWeight(i), i-1, gaussianWeight(i-1))
		}
	}
}

// A constant series (100 every week) must produce avg = 100 regardless of
// the Gaussian weights. This proves the algorithm does not distort stable data.
func TestWeekdayStatsWeightedConstantSeries(t *testing.T) {
	// 8 consecutive Mondays, each with R$100.
	var entries []domain.FinancialEntry
	for i := 0; i < 8; i++ {
		date := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, i*7) // Mon Jun 1, Jun 8, ...
		entries = append(entries, domain.FinancialEntry{
			TransactionDate: domain.NewCalendarDate(date),
			Amount:          10000, // R$100.00
			Type:            domain.EntryTypeIncome,
			Category:        "venda_balcao",
			Origin:          domain.OriginVenda,
		})
	}
	// Window covers all 8 Mondays.
	from := day(t, "2026-06-01")
	to := day(t, "2026-07-26")

	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-27")) // a Monday

	monday := stats[1]
	if monday.Count != 8 {
		t.Errorf("Count = %d, want 8 distinct Mondays", monday.Count)
	}
	if monday.Avg != 10000 {
		t.Errorf("Avg = %d, want 10000 (constant series must not be distorted)", monday.Avg)
	}
}

// An old outlier (week 7) must barely move the average, while recent normal
// values dominate. This is the core reason for Gaussian weighting.
func TestWeekdayStatsWeightedOutlierSuppression(t *testing.T) {
	// 7 recent Mondays at R$100, 1 old Monday (week 7) at R$5000.
	var entries []domain.FinancialEntry
	// The recent normal series: June 1 through July 6 (Mondays).
	for i := 0; i < 7; i++ {
		date := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, i*7)
		entries = append(entries, domain.FinancialEntry{
			TransactionDate: domain.NewCalendarDate(date),
			Amount:          10000,
			Type:            domain.EntryTypeIncome,
			Category:        "venda_balcao",
			Origin:          domain.OriginVenda,
		})
	}
	// The old outlier: May 25 (7 weeks before July 13, week 7).
	entries = append(entries, domain.FinancialEntry{
		TransactionDate: day(t, "2026-05-25"), // a Monday
		Amount:          500000,
		Type:            domain.EntryTypeIncome,
		Category:        "venda_balcao",
		Origin:          domain.OriginVenda,
	})

	// Window ends at July 13 so weekStart(July 13) = July 13 (week 0).
	// Loop produces: Jun 1(6), Jun 8(5), Jun 15(4), Jun 22(3), Jun 29(2),
	// Jul 6(1), Jul 13(0) = 7 recent + May 18(7) = 8 total.
	from := day(t, "2026-05-25")
	to := day(t, "2026-07-13")

	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-14")) // a Tuesday

	monday := stats[1]
	if monday.Count != 8 {
		t.Errorf("Count = %d, want 8", monday.Count)
	}
	// Simple average would be (7*10000 + 500000) / 8 = 71250.
	// Gaussian average must be much closer to 10000 because the outlier is old
	// (week 7, weight ≈ 0.01) while all normal entries are recent (weeks 0–6).
	if monday.Avg > 20000 {
		t.Errorf("Avg = %d, want << 20000 — old outlier must be suppressed", monday.Avg)
	}
	if monday.Avg < 10000 {
		t.Errorf("Avg = %d, want >= 10000 — the outlier still contributes something", monday.Avg)
	}
}

// A recent outlier must dominate the average — the algorithm is responsive to
// trend changes, not frozen in the past.
func TestWeekdayStatsWeightedRecencyBias(t *testing.T) {
	// 7 old Mondays at R$100, 1 recent Monday at R$5000.
	var entries []domain.FinancialEntry
	for i := 0; i < 7; i++ {
		date := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC).AddDate(0, 0, i*7)
		entries = append(entries, domain.FinancialEntry{
			TransactionDate: domain.NewCalendarDate(date),
			Amount:          10000,
			Type:            domain.EntryTypeIncome,
			Category:        "venda_balcao",
			Origin:          domain.OriginVenda,
		})
	}
	entries = append(entries, domain.FinancialEntry{
		TransactionDate: day(t, "2026-07-06"), // a Monday (most recent)
		Amount:          500000,
		Type:            domain.EntryTypeIncome,
		Category:        "venda_balcao",
		Origin:          domain.OriginVenda,
	})

	from := day(t, "2026-05-18")
	to := day(t, "2026-07-12")
	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-13"))

	monday := stats[1]
	// The recent outlier (500000 at offset 0, w=1.0) must pull the average
	// well above the old series (10000 at offsets 1–7).
	if monday.Avg < 50000 {
		t.Errorf("Avg = %d, want >> 50000 — recent outlier must dominate", monday.Avg)
	}
}

func TestWeekdayStatsWeightedBasisThresholds(t *testing.T) {
	// No data → sem_base.
	empty := weekdayStatsWeighted(nil, day(t, "2026-06-15"), day(t, "2026-07-14"), at12(t, "2026-07-15"))
	if empty[1].Basis != ProjectionNoBasis {
		t.Errorf("empty Monday basis = %q, want %q", empty[1].Basis, ProjectionNoBasis)
	}

	// 3 distinct weeks → parcial.
	var few []domain.FinancialEntry
	for i := 0; i < 3; i++ {
		date := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC).AddDate(0, 0, i*7)
		few = append(few, domain.FinancialEntry{
			TransactionDate: domain.NewCalendarDate(date),
			Amount:          10000,
			Type:            domain.EntryTypeIncome,
			Category:        "venda_balcao",
			Origin:          domain.OriginVenda,
		})
	}
	fewStats := weekdayStatsWeighted(few, day(t, "2026-06-15"), day(t, "2026-07-14"), at12(t, "2026-07-15"))
	if fewStats[1].Basis != ProjectionPartial {
		t.Errorf("3-week Monday basis = %q, want %q", fewStats[1].Basis, ProjectionPartial)
	}

	// 7 distinct weeks → janela.
	var many []domain.FinancialEntry
	for i := 0; i < 7; i++ {
		date := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC).AddDate(0, 0, i*7)
		many = append(many, domain.FinancialEntry{
			TransactionDate: domain.NewCalendarDate(date),
			Amount:          10000,
			Type:            domain.EntryTypeIncome,
			Category:        "venda_balcao",
			Origin:          domain.OriginVenda,
		})
	}
	manyStats := weekdayStatsWeighted(many, day(t, "2026-05-18"), day(t, "2026-07-14"), at12(t, "2026-07-15"))
	if manyStats[1].Basis != ProjectionFromWindow {
		t.Errorf("7-week Monday basis = %q, want %q", manyStats[1].Basis, ProjectionFromWindow)
	}
}

func TestWeekdayStatsWeightedIsToday(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-06", 10000), // a Monday
	}
	from := day(t, "2026-06-15")
	to := day(t, "2026-07-14")

	// On a Wednesday, Monday is not today.
	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-15"))
	if stats[1].IsToday {
		t.Error("Monday should not be today on a Wednesday")
	}
	if !stats[3].IsToday {
		t.Error("Wednesday should be flagged as today")
	}
}

func TestWeekdayStatsWeightedExpensesExcluded(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-06", 10000),
		expense(t, "2026-07-06", "aluguel", 90000),
	}
	from := day(t, "2026-06-15")
	to := day(t, "2026-07-14")

	stats := weekdayStatsWeighted(entries, from, to, at12(t, "2026-07-15"))
	monday := stats[1]
	if monday.Avg != 10000 {
		t.Errorf("Avg = %d, want 10000 (expense must not count)", monday.Avg)
	}
}
