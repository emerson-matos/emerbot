package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
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
// through a window of fixture sales. Every weekday is marked as observed for one
// week, so the sample reaches seven and the rates read as an ordinary,
// fully-backed projection.
func ratesFor(avgs ...int64) dailyRates {
	rates := dailyRates{weeks: [daysInWeek]int{1, 1, 1, 1, 1, 1, 1}}
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

func TestRecentFactorUsesMedianRatherThanAnExceptionalDay(t *testing.T) {
	from := day(t, "2026-06-16")
	to := day(t, "2026-08-10")
	entries := make([]domain.FinancialEntry, 0, recentRegimeDays)
	start := to.Time().AddDate(0, 0, -(recentRegimeDays - 1))
	for current := start; !current.After(to.Time()); current = current.AddDate(0, 0, 1) {
		amount := int64(110000)
		if current.Format("2006-01-02") == "2026-08-01" {
			amount = 470000 // must not move the median from the current rhythm.
		}
		entries = append(entries, sale(t, current.Format("2006-01-02"), amount))
	}

	factor, observations, ok := recentFactor(entries, ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000), from, to)
	if !ok || observations != recentRegimeDays {
		t.Fatalf("recentFactor availability = %v/%d, want true/%d", ok, observations, recentRegimeDays)
	}
	if factor != 1.1 {
		t.Errorf("factor = %.2f, want 1.10", factor)
	}
}

func TestProjectedCloseSharesOfficialArithmetic(t *testing.T) {
	monthClock := clock(t, "2026-07", "2026-07-27")
	rates := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	remaining, projected := projectedClose(rates, 1, monthClock, 1000000, 25000, monthClock.today)
	official := buildProjection(rates, GoalProgress{RevenueActual: 1000000, DaysRemaining: monthClock.remaining}, monthClock, 25000)
	if remaining != official.Remaining || projected != official.Projected {
		t.Errorf("shared close = %d/%d, official = %d/%d", remaining, projected, official.Remaining, official.Projected)
	}
}

func TestWeekdayForecastErrorsAreOutOfSample(t *testing.T) {
	// The error window ends on 10 August. Every earlier day sells R$1.000,00;
	// that Monday sells R$2.000,00. Its baseline must still be R$1.000,00,
	// because the baseline for a day ends the day before it.
	entries := make([]domain.FinancialEntry, 0, 80)
	for current := at12(t, "2026-05-26"); !current.After(at12(t, "2026-08-10")); current = current.AddDate(0, 0, 1) {
		amount := int64(100000)
		if current.Format("2006-01-02") == "2026-08-10" {
			amount = 200000
		}
		entries = append(entries, sale(t, current.Format("2006-01-02"), amount))
	}

	errors := weekdayForecastErrors(entries, at12(t, "2026-08-11"))
	var monday ExperimentWeekdayError
	for _, got := range errors {
		if got.Day == time.Monday {
			monday = got
		}
	}
	if monday.Observations != 3 {
		t.Fatalf("Monday observations = %d, want 3", monday.Observations)
	}
	if monday.MAE != 33333 {
		t.Errorf("Monday MAE = %d, want 33333", monday.MAE)
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

	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-15")) // a Wednesday

	monday := stats[1]
	if monday.Day != time.Monday {
		t.Fatalf("index 1 should be Monday, got %v", monday.Day)
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
	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-15"))

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

	got := expenseComposition(entries, nil)

	if len(got) != 2 {
		t.Fatalf("got %d categories, want 2", len(got))
	}
	if got[0].CategoryID != "aluguel" || got[0].Percentage != 60 {
		t.Errorf("first = %+v, want aluguel at 60%%", got[0])
	}
	if got[1].CategoryName != "Folha de Pagamento" {
		t.Errorf("CategoryName = %q, want the domain label", got[1].CategoryName)
	}
	if len(expenseComposition(nil, nil)) != 0 {
		t.Error("no expenses should yield no composition, not a division by zero")
	}
}

// Faturamento split into the kinds of sale behind it. What counts as a sale is
// still the origin and never the category (ADR-016) — a category called
// "Empréstimos" would not make one, and a loan filed under "Venda Balcão" is
// still not one.
func TestRevenueComposition(t *testing.T) {
	atacado := func(date string, amount int64) domain.FinancialEntry {
		e := sale(t, date, amount)
		e.Category = "venda_atacado"
		return e
	}
	loan := sale(t, "2026-07-04", 900000)
	loan.Origin = domain.OriginEmprestimo

	entries := []domain.FinancialEntry{
		atacado("2026-07-01", 60000),
		atacado("2026-07-02", 20000),
		sale(t, "2026-07-03", 20000), // venda_balcao
		loan,
		expense(t, "2026-07-03", "aluguel", 500000),
	}

	got := revenueComposition(entries, map[string]string{"venda_atacado": "Venda Atacado"})

	if len(got) != 2 {
		t.Fatalf("got %d categories (%+v), want atacado and balcão", len(got), got)
	}
	if got[0].CategoryID != "venda_atacado" || got[0].Amount != 80000 || got[0].Percentage != 80 {
		t.Errorf("first = %+v, want venda_atacado at 80000 and 80%%", got[0])
	}
	// The user's own label, not the slug: they named this category.
	if got[0].CategoryName != "Venda Atacado" {
		t.Errorf("CategoryName = %q, want the label from the catalog", got[0].CategoryName)
	}
	if got[1].CategoryID != "venda_balcao" || got[1].Percentage != 20 {
		t.Errorf("second = %+v, want venda_balcao at 20%% — the empréstimo excluded", got[1])
	}
	if len(revenueComposition(nil, nil)) != 0 {
		t.Error("no sales should yield no composition, not a division by zero")
	}
}

// A breakdown decomposes a total: the parts have to add up to the faturamento
// printed above them, or the page contradicts itself.
func TestRevenueCompositionDecomposesFaturamento(t *testing.T) {
	varejo := sale(t, "2026-07-10", 150000)
	varejo.Category = "venda_varejo"
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-02", 250000),
		varejo,
	}

	got := Build(Input{
		Month:          "2026-07",
		Entries:        entries,
		RevenueEntries: entries,
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(400000, 0)},
		CategoryLabels: map[string]string{"venda_varejo": "Venda Varejo"},
		Now:            at12(t, "2026-07-15"),
	})

	var composed int64
	for _, c := range got.RevenueComposition {
		composed += c.Amount
	}
	if composed != got.KPIs.Faturamento {
		t.Errorf("RevenueComposition totals %d, want it to decompose KPIs.Faturamento (%d)",
			composed, got.KPIs.Faturamento)
	}
	if len(got.RevenueComposition) != 2 {
		t.Fatalf("composition = %+v, want both kinds of sale", got.RevenueComposition)
	}
	if got.RevenueComposition[1].CategoryName != "Venda Varejo" {
		t.Errorf("CategoryName = %q, want the user's label for a category they created",
			got.RevenueComposition[1].CategoryName)
	}
}

// Without a catalog nothing breaks and nothing prints a raw slug: the defaults
// answer for the defaults, and anything else is title-cased.
func TestCompositionNamesCategoriesWithoutACatalog(t *testing.T) {
	varejo := sale(t, "2026-07-10", 100000)
	varejo.Category = "venda_varejo"
	entries := []domain.FinancialEntry{varejo, expense(t, "2026-07-05", "aluguel", 50000)}

	got := Build(Input{
		Month:          "2026-07",
		Entries:        entries,
		RevenueEntries: entries,
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(100000, 50000)},
		Now:            at12(t, "2026-07-15"),
	})

	if got.RevenueComposition[0].CategoryName != "Venda Varejo" {
		t.Errorf("CategoryName = %q, want the slug title-cased", got.RevenueComposition[0].CategoryName)
	}
	if got.ExpenseComposition[0].CategoryName != "Aluguel" {
		t.Errorf("CategoryName = %q, want the default's own label", got.ExpenseComposition[0].CategoryName)
	}
}

// The bot reads the same split, and a ranking that was cut says so (ADR-015).
func TestToolPayloadCarriesRevenueByCategory(t *testing.T) {
	composition := make([]CategoryComposition, 0, maxToolCategories+2)
	for i := range maxToolCategories + 2 {
		composition = append(composition, CategoryComposition{
			CategoryID:   fmt.Sprintf("venda_%d", i),
			CategoryName: fmt.Sprintf("Venda %d", i),
			Amount:       int64(1000 * (maxToolCategories + 2 - i)),
			Percentage:   10,
		})
	}

	full := Analysis{Month: "2026-07", RevenueComposition: composition}.ToolPayload()
	rows, ok := full["faturamento_por_categoria"].([]map[string]any)
	if !ok {
		t.Fatalf("faturamento_por_categoria = %T, want a list", full["faturamento_por_categoria"])
	}
	if len(rows) != maxToolCategories {
		t.Errorf("got %d rows, want the list capped at %d", len(rows), maxToolCategories)
	}
	if full["faturamento_por_categoria_truncado"] != true {
		t.Error("a cut ranking must say it was cut")
	}
	warning, _ := full["faturamento_por_categoria_warning"].(string)
	if !strings.Contains(warning, string(SectionRevenueFull)) {
		t.Errorf("warning = %q, want it to name the section that completes it", warning)
	}

	// And the section brings the rest.
	withSection := Analysis{RevenueComposition: composition}.ToolPayload(SectionRevenueFull)
	if got := withSection[string(SectionRevenueFull)].([]map[string]any); len(got) != len(composition) {
		t.Errorf("section carried %d rows, want all %d", len(got), len(composition))
	}

	// A month whose sales fit is not truncated, and says nothing about it.
	short := Analysis{RevenueComposition: composition[:2]}.ToolPayload()
	if _, ok := short["faturamento_por_categoria_truncado"]; ok {
		t.Error("a complete ranking must not carry a truncation flag")
	}
}

func TestCategoryLabelFallsBackToTitleCase(t *testing.T) {
	if got := categoryLabel(nil, "taxa_maquininha"); got != "Taxa Maquininha" {
		t.Errorf("categoryLabel(unknown slug) = %q, want %q", got, "Taxa Maquininha")
	}
}

func TestGoalProgress(t *testing.T) {
	// 10 July; July has 31 days. The actuals are passed in — the faturamento
	// and the despesa the KPI row reports, so the goal card cannot quote a
	// different total for the same month than the row above it does.
	const (
		revenue int64 = 40000
		expense int64 = 30000
	)

	t.Run("without a goal", func(t *testing.T) {
		got := goalProgress(nil, clock(t, "2026-07", "2026-07-10"), revenue, expense)
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
		got := goalProgress(goal, clock(t, "2026-07", "2026-07-10"), revenue, expense)
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

	// Every other figure here is about the month. Asked what tomorrow looks
	// like, a consumer had only the month-end balance and the month's whole
	// bill list to answer with — see ADR-020.
	t.Run("names tomorrow on its own", func(t *testing.T) {
		// Saturday the 11th: R$300,00 of crediário booked to land, R$500,00 of
		// bills due, and a Saturday that usually takes R$1.000,00.
		rates := ratesFor(0, 0, 0, 0, 0, 0, 100000)
		points := []pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: 15000},
			{Date: "2026-07-11", ProjectedIncome: 30000, ProjectedExpense: 50000, RunningBalance: -5000},
			{Date: "2026-07-31", RunningBalance: -5000},
		}

		got := buildCashPosition(points, rates, now)

		next := got.NextDay
		if next == nil {
			t.Fatal("NextDay = nil, want tomorrow's own line of the runway")
		}
		if next.Date != "2026-07-11" {
			t.Errorf("Date = %q, want tomorrow", next.Date)
		}
		if next.ScheduledIn != 30000 || next.ScheduledOut != 50000 {
			t.Errorf("scheduled = +%d/-%d, want what is booked for the day", next.ScheduledIn, next.ScheduledOut)
		}
		// A Saturday brings R$1.000,00 and R$300,00 of it is already booked, so
		// R$700,00 is still expected on top — the day is not counted twice.
		if next.ExpectedIn != 70000 {
			t.Errorf("ExpectedIn = %d, want a Saturday's takings net of what it has booked (70000)", next.ExpectedIn)
		}
		// The booked curve dives to -5000; crediting the day turns it positive,
		// which is the whole reason the runway is projected rather than booked.
		if next.Balance != 65000 {
			t.Errorf("Balance = %d, want the booked -5000 plus the 70000 expected", next.Balance)
		}
	})

	// The forecast covers one calendar month, so the last day of it has no
	// tomorrow inside — and neither does a month already closed. A row of
	// zeroes there would read as a day with no money moving.
	t.Run("no tomorrow to name at the end of the month", func(t *testing.T) {
		points := []pkgfinance.CashFlowPoint{{Date: "2026-07-31", RunningBalance: 15000}}
		if got := buildCashPosition(points, noRates, at12(t, "2026-07-31")); got.NextDay != nil {
			t.Errorf("NextDay = %+v, want nil on the month's last day", got.NextDay)
		}
	})

	// A total of bills is not a finding. Asked "como estamos?", the bot reported
	// "o volume de despesas agendadas (R$ 19.130,95) é um ponto de atenção" on a
	// month whose runway never went near zero — the amount was offered with
	// nothing to weigh it against. See ADR-022.
	t.Run("grades the month's commitments against the curve", func(t *testing.T) {
		rates := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)

		covered := buildCashPosition([]pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: 500000},
			{Date: "2026-07-11", ProjectedExpense: 200000, RunningBalance: 300000},
		}, rates, now)
		if covered.Commitments != CommitmentsCovered {
			t.Errorf("Commitments = %q, want %q — the balance never goes under water",
				covered.Commitments, CommitmentsCovered)
		}

		uncovered := buildCashPosition([]pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: 50000},
			{Date: "2026-07-11", ProjectedExpense: 900000, RunningBalance: -850000},
		}, rates, now)
		if uncovered.Commitments != CommitmentsUncovered {
			t.Errorf("Commitments = %q, want %q", uncovered.Commitments, CommitmentsUncovered)
		}

		// A balance already under water today is the worst case to grade
		// "coberto", and DaysUntilNegative alone would: it only counts crossings
		// still ahead. The trough is what this reads.
		alreadyNegative := buildCashPosition([]pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: -1000},
		}, rates, now)
		if alreadyNegative.Commitments != CommitmentsUncovered {
			t.Errorf("Commitments = %q, want %q for a balance already negative today",
				alreadyNegative.Commitments, CommitmentsUncovered)
		}

		// No trading history: the forward curve is bills against nothing, so it
		// cannot answer — and must not report the bills as unpayable.
		blind := buildCashPosition([]pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: 50000},
			{Date: "2026-07-11", ProjectedExpense: 900000, RunningBalance: -850000},
		}, noRates, now)
		if blind.Commitments != CommitmentsUnknown {
			t.Errorf("Commitments = %q, want %q with no history to project from",
				blind.Commitments, CommitmentsUnknown)
		}
	})

	// The dashboard drew the *booked* curve and captioned its tail "projeção",
	// which dives every month by construction — all the bills from the 1st and
	// none of the sales. The series that answers it is the one this function
	// already walks; it just used to be thrown away. See ADR-021.
	t.Run("keeps the whole curve, realised half and projected half", func(t *testing.T) {
		rates := ratesFor(0, 0, 0, 0, 0, 0, 100000) // a Saturday brings R$1.000,00
		points := []pkgfinance.CashFlowPoint{
			{Date: "2026-07-09", RunningBalance: 20000},
			{Date: "2026-07-10", RunningBalance: 15000},
			{Date: "2026-07-11", ProjectedExpense: 50000, RunningBalance: -35000},
		}

		got := buildCashPosition(points, rates, now)

		if len(got.Forecast) != len(points) {
			t.Fatalf("Forecast has %d days, want one per point", len(got.Forecast))
		}
		// Before today nothing is credited, so the series is the booked curve
		// there — which is what lets one series draw both halves of the chart.
		if got.Forecast[0].Balance != 20000 {
			t.Errorf("Forecast[0].Balance = %d, want the booked balance before today", got.Forecast[0].Balance)
		}
		// Saturday the 11th: booked at -35000, credited with a Saturday's
		// takings, so the curve does not dive.
		if got.Forecast[2].Balance != 65000 {
			t.Errorf("Forecast[2].Balance = %d, want the booked -35000 plus a Saturday's 100000", got.Forecast[2].Balance)
		}
		// The scalars are readings of this series, not a second walk over it.
		if got.EndOfMonthProjection != got.Forecast[len(got.Forecast)-1].Balance {
			t.Errorf("EndOfMonthProjection = %d, want the curve's last balance (%d)",
				got.EndOfMonthProjection, got.Forecast[len(got.Forecast)-1].Balance)
		}
		if got.NextDay == nil || *got.NextDay != got.Forecast[2] {
			t.Errorf("NextDay = %+v, want the same entry the series holds for tomorrow", got.NextDay)
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
	if want := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday}; !slices.Equal(got.Days, want) {
		t.Errorf("Days = %v, want one per elapsed day this week (%v)", got.Days, want)
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
	if len(got.Days) != daysInWeek {
		t.Errorf("Days = %v, want all seven days by Sunday", got.Days)
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

// The warning and the advice about the same fall are one decision, not two.
//
// They used to be two: the insight divided its own percentage from `compared`
// with a raw denominator and no dead band, the recommendation read the rounded
// trend, and each had its own threshold constant. At the boundary a month got
// "Faturamento caiu" with no advice attached — -10.4% is past -10 as a float and
// exactly -10 once rounded.
func TestRevenueDropWarnsAndAdvisesTogether(t *testing.T) {
	has := func(msgs []Insight, want InsightType) bool {
		for _, m := range msgs {
			if m.Type == want {
				return true
			}
		}
		return false
	}
	advised := func(recs []Recommendation) bool {
		for _, r := range recs {
			if r.Title == "Receita caiu" {
				return true
			}
		}
		return false
	}

	// Sweep the whole range either side of the threshold, at odd totals so the
	// rounding boundary is actually crossed rather than stepped over.
	for _, previous := range []int64{100000, 333333, 987654} {
		for current := previous / 2; current <= previous; current += previous / 53 {
			compared := comparison{
				current:  monthTotals{revenue: current, income: current, expense: 1000, balance: current - 1000},
				previous: monthTotals{revenue: previous, income: previous, expense: 1000, balance: previous - 1000},
				clock:    clock(t, "2026-06", "2026-07-10"),
			}
			trends := buildTrends(compared)
			health := buildHealth(nil, compared, trends, WeekComparison{}, Projection{})
			recs := buildRecommendations(WeekComparison{}, Projection{}, trends, CashPosition{}, compared)

			warned, advice := has(health.Messages, InsightRevenueDrop), advised(recs)
			if warned != advice {
				t.Fatalf("%d→%d (%d%%): insight=%v recommendation=%v — the two must fire together",
					previous, current, trends.Faturamento.Change, warned, advice)
			}
			// And the figure the warning quotes is the one the page prints.
			if warned && !strings.HasPrefix(health.Messages[len(health.Messages)-1].Description,
				fmt.Sprintf("%d%%", -trends.Faturamento.Change)) {
				t.Fatalf("%d→%d: warning does not quote trends.Faturamento.Change (%d)",
					previous, current, trends.Faturamento.Change)
			}
		}
	}
}

// The pace insight and the pace card read one verdict. The insight applied a
// ±5 dead band and the card applied none, so a week inside the band showed an
// arrow and a percentage next to insights calling it flat.
func TestWeekPaceVerdictIsShared(t *testing.T) {
	for _, tc := range []struct {
		name              string
		current, previous int64
		wantDirection     TrendDirection
		wantInsight       InsightType
	}{
		{"inside the dead band", 103000, 100000, TrendStable, ""},
		{"a real rise", 120000, 100000, TrendUp, InsightWeeklyImprovement},
		{"a real fall", 80000, 100000, TrendDown, InsightWeeklyDecline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := []domain.FinancialEntry{
				sale(t, "2026-07-27", tc.current),  // Monday of this week
				sale(t, "2026-07-20", tc.previous), // the Monday before
			}
			// A Tuesday: Monday is the one finished day on both sides.
			week := buildWeekComparison(entries, at12(t, "2026-07-28"), 0)

			if week.Pace.Direction != tc.wantDirection {
				t.Errorf("Direction = %q, want %q (change %d%%)", week.Pace.Direction, tc.wantDirection, week.Pace.Change)
			}

			health := buildHealth(nil, comparison{clock: clock(t, "2026-07", "2026-07-28")}, Trends{}, week, Projection{})
			var got InsightType
			for _, m := range health.Messages {
				if m.Type == InsightWeeklyImprovement || m.Type == InsightWeeklyDecline {
					got = m.Type
				}
			}
			if got != tc.wantInsight {
				t.Errorf("insight = %q, want %q — the card and the insight share one dead band", got, tc.wantInsight)
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

	health := buildHealth(nil, compared, buildTrends(compared), WeekComparison{}, Projection{})

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
	// 10 of 30 days gone, R$1.000,00 of a R$10.000,00 target: R$700.000
	// still short, and the projection misses.
	projection := Projection{
		Actual: 100000, Projected: 300000, Target: 1000000,
		Gap: 700000, DaysRemaining: 20,
	}
	// Mid-month, with a day already behind us, so the pacing insight is the
	// only thing this test is looking at.
	midMonth := comparison{clock: clock(t, "2026-07", "2026-07-11")}
	health := buildHealth(nil, midMonth, buildTrends(midMonth), WeekComparison{}, projection)

	var behind *Insight
	for i, m := range health.Messages {
		if m.Type == InsightGoalBehind {
			behind = &health.Messages[i]
		}
	}
	if behind == nil {
		t.Fatalf("expected a goal-behind insight, got %+v", health.Messages)
	}
	if behind.Description != "A projeção indica fechamento abaixo da meta." {
		t.Errorf("description = %q, want neutral projection message", behind.Description)
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

func TestRecommendationsProjectionCoverageMatrix(t *testing.T) {
	// Coverage is Projected / Target — the one number that decides the
	// recommendation title. The old test varied week-over-week pace; the new
	// recommendation ignores it in favour of the projection's own verdict.
	tests := []struct {
		name        string
		projection  Projection
		want        string
		wantMessage string
	}{
		{
			"covers the target",
			Projection{Projected: 1000000, Target: 1000000, Coverage: 1.00, Status: ProjSuccess, OnTrack: true, DaysRemaining: 20},
			"Ritmo suficiente", "Manter o desempenho",
		},
		{
			"short but within reach",
			Projection{Projected: 900000, Target: 1000000, Coverage: 0.90, Status: ProjWarning, DaysRemaining: 20},
			"Projeção abaixo da meta", "aumentar as vendas",
		},
		{
			"needs a hard acceleration",
			Projection{Projected: 600000, Target: 1000000, Coverage: 0.60, Status: ProjDanger, DaysRemaining: 20},
			"Projeção muito abaixo da meta", "aceleração consistente",
		},
		{
			"out of reach",
			Projection{Projected: 300000, Target: 1000000, Coverage: 0.30, Status: ProjDanger, DaysRemaining: 20},
			"Projeção muito abaixo da meta", "muito acima do histórico recente",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recs := buildRecommendations(WeekComparison{}, tc.projection, Trends{}, CashPosition{}, comparison{})
			if len(recs) == 0 {
				t.Fatal("expected a recommendation")
			}
			if recs[0].Title != tc.want {
				t.Errorf("title = %q, want %q", recs[0].Title, tc.want)
			}
			// The two ProjDanger rows share a title and differ only in wording,
			// so the message is the only thing that tells them apart.
			if !strings.Contains(recs[0].Message, tc.wantMessage) {
				t.Errorf("message = %q, want it to mention %q", recs[0].Message, tc.wantMessage)
			}
		})
	}
}

func TestProjectionStatusBoundaries(t *testing.T) {
	// Against the function that owns the cuts: reading them off a Projection
	// built from a float ratio would test Go's rounding as much as the cuts.
	tests := []struct {
		coverage float64
		want     ProjectionStatus
	}{
		{1.20, ProjSuccess},
		{1.00, ProjSuccess},
		{0.95, ProjSuccess},
		{0.9499, ProjWarning},
		{0.80, ProjWarning},
		{0.7999, ProjDanger},
		{0.50, ProjDanger},
		{0.00, ProjDanger},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("coverage=%.4f", tc.coverage), func(t *testing.T) {
			if got := projectionStatus(tc.coverage); got != tc.want {
				t.Errorf("projectionStatus(%v) = %q, want %q", tc.coverage, got, tc.want)
			}
		})
	}
}

// A month with no goal has no verdict to give. Coverage is 0 there, which the
// thresholds alone would read as the worst possible month.
func TestProjectionWithoutATargetHasNoStatus(t *testing.T) {
	got := buildProjection(fridayOnly, GoalProgress{RevenueActual: 50000, DaysRemaining: 6}, clock(t, "2026-07", "2026-07-26"), 0)

	if got.Status != ProjNoTarget || got.Coverage != 0 {
		t.Errorf("Status = %q, Coverage = %v, want no verdict without a target", got.Status, got.Coverage)
	}
	if recs := buildRecommendations(WeekComparison{}, got, Trends{}, CashPosition{}, comparison{}); len(recs) != 0 {
		t.Errorf("recommendations = %+v, want none without a target", recs)
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
	rates := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	// The 26th through the 31st is six days, today included.
	goals := GoalProgress{
		RevenueTarget: 3600000, RevenueActual: 2777500,
		DaysTotal: 31, DaysRemaining: 6,
	}

	got := buildProjection(rates, goals, clock(t, "2026-07", "2026-07-26"), 0)

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
}

func TestProjectionCountsTodayAsADayStillToTrade(t *testing.T) {
	// 31 July, a Friday, and the shop is open. The projection used to start at
	// *tomorrow*, so the last day of every month reported nothing left to
	// project and no per-day ask — the day was written off before it happened.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 900000, DaysTotal: 31, DaysRemaining: 1}

	got := buildProjection(fridayOnly, goals, clock(t, "2026-07", "2026-07-31"), 0)

	if want := int64(100000); got.Remaining != want {
		t.Errorf("Remaining = %d, want today's Friday average (%d)", got.Remaining, want)
	}
	if !got.OnTrack {
		t.Error("OnTrack = false, want true — an ordinary Friday closes the gap")
	}
}

// A day that is already half traded must not be counted twice: what has sold
// today is in Actual, so only the rest of an ordinary day is still ahead.
func TestProjectionDoesNotCountTodayTwice(t *testing.T) {
	goals := GoalProgress{RevenueActual: 900000, DaysTotal: 31, DaysRemaining: 1}

	got := buildProjection(fridayOnly, goals, clock(t, "2026-07", "2026-07-31"), 40000)

	if want := int64(60000); got.Remaining != want {
		t.Errorf("Remaining = %d, want the rest of today (%d)", got.Remaining, want)
	}

	// And a day that has already beaten its average has nothing left to add,
	// rather than a negative amount.
	beaten := buildProjection(fridayOnly, goals, clock(t, "2026-07", "2026-07-31"), 150000)
	if beaten.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0 once today has passed its average", beaten.Remaining)
	}
}

func TestProjectionHasNothingLeftForAClosedMonth(t *testing.T) {
	// Analysing July in August: the month is over, so there is no day left to
	// project into and no per-day ask to make of it.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 900000, DaysTotal: 31}

	got := buildProjection(fridayOnly, goals, clock(t, "2026-07", "2026-08-03"), 0)

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
	// And nothing was estimated: Projected is July's own faturamento. Reading
	// the basis off the rates called that "janela" — an eight-week estimate,
	// stamped on the one figure in this struct that is not an estimate — and the
	// card captioned a realised total as a forecast.
	if got.Basis != ProjectionClosed {
		t.Errorf("Basis = %q, want %q for a month that has already ended", got.Basis, ProjectionClosed)
	}
	// It says so even when no window was fetched, so a caller that skips the
	// read for a closed month cannot turn the label into "sem_base".
	unfetched := buildProjection(dailyRates{}, goals, clock(t, "2026-07", "2026-08-03"), 0)
	if unfetched.Basis != ProjectionClosed {
		t.Errorf("Basis = %q without a window, want %q", unfetched.Basis, ProjectionClosed)
	}
}

// TodayTarget is today's weekday average scaled by what it would take to close
// the gap if every remaining day pulled the same weight.
func TestDayTargetScalesTodaysOwnWeekdayAverage(t *testing.T) {
	// Monday 2026-07-27: five days left in July, all priced at R$1.000,00.
	flat := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	// R$7.500,00 still to find over five days worth R$5.000,00 at the usual
	// rhythm: every day has to bring 1,5× what it normally does.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 250000, DaysTotal: 31, DaysRemaining: 5}

	got := buildProjection(flat, goals, clock(t, "2026-07", "2026-07-27"), 0).TodayTarget

	if !got.Asked() {
		t.Fatalf("TodayTarget = %+v, want a target on an ordinary Monday", got)
	}
	if got.Day != time.Monday {
		t.Errorf("Day = %v, want the day being asked about", got.Day)
	}
	if got.Historical != 100000 {
		t.Errorf("Historical = %d, want Monday's average (100000)", got.Historical)
	}
	if got.Factor != 1.5 {
		t.Errorf("Factor = %v, want 1.5", got.Factor)
	}
	if got.Target != 150000 {
		t.Errorf("Target = %d, want the average at 1,5× (150000)", got.Target)
	}
	if got.Delta != 50000 {
		t.Errorf("Delta = %d, want the stretch over an ordinary Monday (50000)", got.Delta)
	}
	if got.Status != PaceAbove {
		t.Errorf("Status = %q, want %q", got.Status, PaceAbove)
	}
}

// The share is weighted by the weekday, not spread flat: the whole difference
// from a simple average, which would ask a Saturday for as much as a Monday.
func TestTodayTargetFollowsTheWeekdayRhythm(t *testing.T) {
	// Saturday 2026-07-25 and Monday 2026-07-27, against the same rates.
	rates := ratesFor(0, 200000, 200000, 200000, 200000, 200000, 100000)
	goals := func(remaining int) GoalProgress {
		return GoalProgress{RevenueTarget: 2000000, RevenueActual: 0, DaysTotal: 31, DaysRemaining: remaining}
	}

	sat := buildProjection(rates, goals(7), clock(t, "2026-07", "2026-07-25"), 0).TodayTarget
	mon := buildProjection(rates, goals(5), clock(t, "2026-07", "2026-07-27"), 0).TodayTarget

	if sat.Historical != 100000 || mon.Historical != 200000 {
		t.Fatalf("Historical: Sat = %d, Mon = %d — want the weekday averages", sat.Historical, mon.Historical)
	}
	if sat.Target >= mon.Target {
		t.Errorf("Sat target = %d, Mon target = %d, want Saturday asked for less", sat.Target, mon.Target)
	}
}

// A day already half traded must not deflate its own target: the historical
// average it is compared against is a whole day's takings.
func TestTodayTargetHoldsStillAsTheDayIsTraded(t *testing.T) {
	flat := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)

	// Same morning, told twice: nothing sold yet, then R$1.000,00 already
	// through the till and folded into Actual.
	opening := GoalProgress{RevenueTarget: 1000000, RevenueActual: 250000, DaysTotal: 31, DaysRemaining: 5}
	midday := GoalProgress{RevenueTarget: 1000000, RevenueActual: 350000, DaysTotal: 31, DaysRemaining: 5}

	before := buildProjection(flat, opening, clock(t, "2026-07", "2026-07-27"), 0).TodayTarget
	after := buildProjection(flat, midday, clock(t, "2026-07", "2026-07-27"), 100000).TodayTarget

	if before.Target != after.Target || before.Factor != after.Factor {
		t.Errorf("target moved through the day: %d (×%v) then %d (×%v), want the same whole-day ask",
			before.Target, before.Factor, after.Target, after.Factor)
	}
}

// The floor: a month running ahead is asked for its ordinary rhythm, never for
// less. This used to assert the opposite — the gap was distributed exactly, so
// a Monday worth R$ 1.000,00 came back asking for R$ 500,00 and the digest told
// the pharmacy at seven in the morning that it could take the day lighter. A
// target is a floor under the day, not a ceiling over it (ADR-025).
func TestTodayTargetNeverFallsBelowTheAverage(t *testing.T) {
	flat := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	// R$2.500,00 left over five days worth R$5.000,00 — the gap alone would
	// have asked half an ordinary day of each.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 750000, DaysTotal: 31, DaysRemaining: 5}

	projection := buildProjection(flat, goals, clock(t, "2026-07", "2026-07-27"), 0)
	got := projection.TodayTarget

	if !got.Asked() {
		t.Fatalf("TodayTarget = %+v, want a target — a day at the usual rhythm is still an ask", got)
	}
	if got.Target != 100000 {
		t.Errorf("Target = %d, want Monday's own average (100000)", got.Target)
	}
	if got.Factor != 1 {
		t.Errorf("Factor = %v, want the floor at 1", got.Factor)
	}
	if got.Delta != 0 || got.Status != PaceOnTrack {
		t.Errorf("TodayTarget = %+v, want the ask level with the weekday average", got)
	}
	// The month being ahead is still said — it moved from the amount to the
	// factor the gap alone would have asked for.
	if projection.Plan.GapFactor != 0.5 {
		t.Errorf("GapFactor = %v, want 0.5 — the gap alone asked for half a day", projection.Plan.GapFactor)
	}
	if got.Source != TargetFromAverage {
		t.Errorf("Source = %q, want %q", got.Source, TargetFromAverage)
	}
}

// A goal already met stops the asking but not the selling: the day is asked for
// its own rhythm, and the good news travels as the source of that ask. It used
// to come back with no target at all, which read as the same blank as "não há
// histórico" — and a pharmacy that beat its goal on the 20th was asked for
// nothing for eleven days.
func TestGoalMetStillAsksForTheUsualDay(t *testing.T) {
	flat := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 1200000, DaysTotal: 31, DaysRemaining: 5}

	projection := buildProjection(flat, goals, clock(t, "2026-07", "2026-07-27"), 0)
	got := projection.TodayTarget

	if !got.Asked() {
		t.Fatalf("TodayTarget = %+v, want the usual day asked for", got)
	}
	if got.Target != 100000 || got.Historical != 100000 {
		t.Errorf("TodayTarget = %+v, want the ask at Monday's average", got)
	}
	if got.Source != TargetGoalMet {
		t.Errorf("Source = %q, want %q — the ask and the reason for it are different things",
			got.Source, TargetGoalMet)
	}
	if projection.Plan.GapFactor != 0 {
		t.Errorf("GapFactor = %v, want 0 — there is nothing left to catch up on", projection.Plan.GapFactor)
	}
}

// A window with nothing in it has no floor to stand on either, and that answer
// outranks a goal already met: the check used to sit *after* the goal-met
// short-circuit, so a ledger that had never traded but carried a beaten target
// reported "meta_batida" over averages it did not have.
func TestNoHistoryOutranksAGoalAlreadyMet(t *testing.T) {
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 1200000, DaysTotal: 31, DaysRemaining: 5}

	got := buildProjection(dailyRates{}, goals, clock(t, "2026-07", "2026-07-27"), 0).TodayTarget

	if got.State != DayTargetNoHistory {
		t.Errorf("State = %q, want %q", got.State, DayTargetNoHistory)
	}
}

// The cases with no honest answer, each named rather than collapsed into one
// silence. They used to share a single `valid: false`, which is how "a meta já
// foi batida" — good news — reached the reader as the same blank as "não há
// histórico", and left the bot with nothing to say but a number it invented.
func TestTodayTargetNamesWhyThereIsNoAsk(t *testing.T) {
	flat := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 250000, DaysTotal: 31, DaysRemaining: 5}

	tests := []struct {
		name       string
		rates      dailyRates
		goals      GoalProgress
		month, now string
		want       DayTargetState
	}{
		// An ordinary Monday, for contrast: this is what a real ask looks like.
		{"an ordinary trading day", flat, goals, "2026-07", "2026-07-27", DayTargetOK},
		{"no window to price the days from", dailyRates{}, goals, "2026-07", "2026-07-27", DayTargetNoHistory},
		// Sunday 2026-07-26 priced at nothing: the shop does not open, so there
		// is no rhythm to scale and asking a multiple of zero is not an answer.
		// It is emphatically not "sem_historico" — the other six days are known.
		{
			"a weekday the pharmacy does not trade",
			ratesFor(0, 100000, 100000, 100000, 100000, 100000, 100000),
			goals, "2026-07", "2026-07-26", DayTargetClosedWeekday,
		},
		// A goal already met is deliberately *not* in this list: since ADR-025 it
		// is an ask at the weekday's own rhythm, not an absence. See
		// TestGoalMetStillAsksForTheUsualDay.
		{
			"no goal to take a share of", flat,
			GoalProgress{RevenueActual: 250000, DaysTotal: 31, DaysRemaining: 5},
			"2026-07", "2026-07-27", DayTargetNoGoal,
		},
		{"a closed month", flat, goals, "2026-07", "2026-08-03", DayTargetClosedMonth},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildProjection(tc.rates, tc.goals, clock(t, tc.month, tc.now), 0).TodayTarget

			if got.State != tc.want {
				t.Errorf("State = %q, want %q", got.State, tc.want)
			}
			if asked := got.State == DayTargetOK; got.Asked() != asked {
				t.Errorf("Asked() = %t, want %t for state %q", got.Asked(), asked, got.State)
			}
			// Every absence leaves the amounts at zero, so a consumer that
			// renders one without checking the state cannot print a target.
			if !got.Asked() && (got.Target != 0 || got.Historical != 0) {
				t.Errorf("TodayTarget = %+v, want no amounts under state %q", got, got.State)
			}
		})
	}

	// The weekday is filled in even where there is no ask: "a farmácia não abre
	// domingo" needs to know it is Sunday it is talking about.
	sunday := buildProjection(ratesFor(0, 100000, 100000, 100000, 100000, 100000, 100000),
		goals, clock(t, "2026-07", "2026-07-26"), 0).TodayTarget
	if sunday.Day != time.Sunday {
		t.Errorf("Day = %v, want Sunday named even with no ask", sunday.Day)
	}
}

// Any day of the month is priced by the same plan: one factor, each day at its
// own weekday rhythm. It briefly existed as a field for tomorrow, which is the
// same mistake with one more day on it — see ADR-021.
func TestPlanPricesAnyDayAtItsOwnRhythm(t *testing.T) {
	// Monday 2026-07-27, five days left. A Tuesday is worth half a Monday here,
	// which is the whole point: the two asks must differ by the rhythm and by
	// nothing else.
	rates := ratesFor(100000, 200000, 100000, 200000, 200000, 200000, 200000)
	goals := GoalProgress{RevenueTarget: 2000000, RevenueActual: 500000, DaysTotal: 31, DaysRemaining: 5}
	monthClock := clock(t, "2026-07", "2026-07-27")

	got := buildProjection(rates, goals, monthClock, 0)
	today := got.TodayTarget
	next := got.Plan.at(rates, monthClock, 28, 0)

	if !next.Asked() {
		t.Fatalf("tomorrow = %+v, want an ask for an ordinary Tuesday", next)
	}
	if next.Day != time.Tuesday || next.Date != "2026-07-28" {
		t.Errorf("tomorrow = %+v, want the day named", next)
	}
	if today.Date != "2026-07-27" {
		t.Errorf("TodayTarget.Date = %q, want today named", today.Date)
	}
	// The same plan, not a second forecast. A different factor would mean two
	// rival distributions of one gap.
	if next.Factor != today.Factor {
		t.Errorf("factors differ: today ×%v, tomorrow ×%v — want one plan", today.Factor, next.Factor)
	}
	if next.Historical != 100000 {
		t.Errorf("Historical = %d, want Tuesday's own average", next.Historical)
	}
	if want := roundToInt64(float64(next.Historical) * today.Factor); next.Target != want {
		t.Errorf("Target = %d, want Tuesday's average at the plan's factor (%d)", next.Target, want)
	}
	// A Tuesday worth half a Monday is asked for half of what the Monday is,
	// to the centavo each figure was rounded to.
	if diff := next.Target*2 - today.Target; diff > 2 || diff < -2 {
		t.Errorf("Tuesday asked %d against Monday's %d, want the weekday rhythm carried through", next.Target, today.Target)
	}
	// Today is being traded, tomorrow is not yet — and the payload has to be
	// able to say which, or a plan reads as a fact.
	if today.Basis != DayInProgress || next.Basis != DayProjected {
		t.Errorf("basis: today %q, tomorrow %q — want em_curso then projetado", today.Basis, next.Basis)
	}
}

// A day that has closed is reported, not asked. Pricing it against today's plan
// would charge a goal to a day nobody can sell on any more — ADR-021.
func TestAClosedDayIsReportedNotAsked(t *testing.T) {
	rates := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 250000, DaysTotal: 31, DaysRemaining: 5}
	monthClock := clock(t, "2026-07", "2026-07-27")

	plan := buildProjection(rates, goals, monthClock, 0).Plan
	// Sunday the 26th, which took R$1.500,00 against a usual R$1.000,00.
	got := plan.at(rates, monthClock, 26, 150000)

	if got.Basis != DayRealized {
		t.Errorf("Basis = %q, want %q for a day that has finished", got.Basis, DayRealized)
	}
	if got.State != DayTargetClosedDay {
		t.Errorf("State = %q, want %q", got.State, DayTargetClosedDay)
	}
	if got.Asked() {
		t.Error("Asked() = true on a closed day — a consumer would print a target for it")
	}
	if got.Target != 0 || got.Factor != 0 {
		t.Errorf("closed day = %+v, want no target invented backwards", got)
	}
	// What it did, beside what a Sunday usually does — the reading that day has.
	if got.Realized != 150000 || got.Historical != 100000 {
		t.Errorf("closed day = %+v, want the result next to the weekday average", got)
	}
	if got.Delta != 50000 || got.Status != PaceAbove {
		t.Errorf("closed day = %+v, want the comparison to run over what it sold", got)
	}
}

// The property that makes the plan a plan: every remaining day asked for its
// own share adds back up to what is missing — or to what those days usually
// bring, whichever is larger. A flat daily average never had the first half —
// it asked a Sunday for a Saturday's money and made up the difference by asking
// a Saturday for less than it already brings (ADR-019) — and the exact
// distribution never had the second, which is what ADR-025 added.
func TestTheRemainingAsksSumToWhatIsMissing(t *testing.T) {
	rates := ratesFor(40000, 120000, 110000, 100000, 120000, 130000, 150000)
	monthClock := clock(t, "2026-07", "2026-07-23")

	sumOfAsks := func(p Projection) int64 {
		var asked int64
		for d := monthClock.today; d <= monthClock.total; d++ {
			asked += p.Plan.at(rates, monthClock, d, 0).Target
		}
		return asked
	}
	var usualRhythm int64
	for d := monthClock.today; d <= monthClock.total; d++ {
		usualRhythm += rates.avg[int(monthClock.weekdayOf(d))]
	}
	// Rounding to the centavo, once per day.
	slack := int64(monthClock.remaining)

	// A month behind its goal: the asks close the gap exactly, as they always did.
	behind := buildProjection(rates,
		GoalProgress{RevenueTarget: 4000000, RevenueActual: 900000, DaysTotal: 31, DaysRemaining: 9},
		monthClock, 0)
	missing := behind.Target - behind.Actual
	if diff := sumOfAsks(behind) - missing; diff > slack || diff < -slack {
		t.Errorf("the days ask for %d against a gap of %d — the plan does not close the month",
			sumOfAsks(behind), missing)
	}
	// And today's own ask is the first slice of that sum, not a separate one.
	if behind.Plan.at(rates, monthClock, monthClock.today, 0) != behind.TodayTarget {
		t.Error("TodayTarget is not the plan's own slice for today")
	}

	// A month ahead of it: the asks add up to the pharmacy's own rhythm, which
	// is more than the gap. Distributing the gap exactly here is what asked the
	// days for less than they habitually sell.
	ahead := buildProjection(rates,
		GoalProgress{RevenueTarget: 4000000, RevenueActual: 3900000, DaysTotal: 31, DaysRemaining: 9},
		monthClock, 0)
	if got := sumOfAsks(ahead); got != usualRhythm {
		t.Errorf("the days ask for %d, want the usual rhythm (%d) when the gap is smaller than it", got, usualRhythm)
	}
}

// The passive projection assumes every day still to come brings what that day
// of the week usually brings — and today counts for a whole ordinary day, not
// for the fraction of one the morning has managed so far. It is the other half
// of "never project below the average": a projection that fell through the day
// would have the month getting worse the longer the shop stayed open.
func TestTheProjectionNeverAssumesLessThanTheAverage(t *testing.T) {
	rates := ratesFor(40000, 120000, 110000, 100000, 120000, 130000, 150000)
	monthClock := clock(t, "2026-07", "2026-07-23")

	var usualRhythm int64
	for d := monthClock.today; d <= monthClock.total; d++ {
		usualRhythm += rates.avg[int(monthClock.weekdayOf(d))]
	}
	// R$ 9.000,00 banked before today, and today told three ways: not open yet,
	// half an ordinary Thursday in, and well past one.
	const banked = 900000
	for _, todayRevenue := range []int64{0, 60000, 400000} {
		goals := GoalProgress{
			RevenueTarget: 4000000, RevenueActual: banked + todayRevenue,
			DaysTotal: 31, DaysRemaining: 9,
		}
		got := buildProjection(rates, goals, monthClock, todayRevenue)

		if want := banked + usualRhythm; got.Projected < want {
			t.Errorf("with %d sold today, Projected = %d, want at least the rhythm (%d)",
				todayRevenue, got.Projected, want)
		}
		// And never below what is already in the till, either.
		if got.Projected < got.Actual {
			t.Errorf("with %d sold today, Projected = %d is below Actual (%d)",
				todayRevenue, got.Projected, got.Actual)
		}
	}
}

// Where the month lands if the asks are met, as against where it lands if the
// days merely trade as usual. The second was published and the first was not,
// so "e se eu bater a meta todo dia?" could only be answered with the month's
// goal — which on a pharmacy running ahead is *below* its own projection
// (ADR-025).
func TestPlannedCloseNeverFallsBelowTheProjection(t *testing.T) {
	rates := ratesFor(40000, 120000, 110000, 100000, 120000, 130000, 150000)
	monthClock := clock(t, "2026-07", "2026-07-23")

	// Behind: meeting every ask lands on the goal, above the passive projection.
	behind := buildProjection(rates,
		GoalProgress{RevenueTarget: 4000000, RevenueActual: 900000, DaysTotal: 31, DaysRemaining: 9},
		monthClock, 0)
	if behind.PlannedClose <= behind.Projected {
		t.Errorf("PlannedClose = %d, want above the passive projection (%d) on a month behind its goal",
			behind.PlannedClose, behind.Projected)
	}
	if diff := behind.PlannedClose - behind.Target; diff > int64(monthClock.remaining) || diff < -int64(monthClock.remaining) {
		t.Errorf("PlannedClose = %d, want the month's goal (%d)", behind.PlannedClose, behind.Target)
	}

	// Ahead: the goal is already the smaller number, and the plan closes the
	// month at the rhythm instead of at the goal.
	ahead := buildProjection(rates,
		GoalProgress{RevenueTarget: 4000000, RevenueActual: 3900000, DaysTotal: 31, DaysRemaining: 9},
		monthClock, 0)
	if ahead.PlannedClose < ahead.Projected {
		t.Errorf("PlannedClose = %d, want at least the passive projection (%d)",
			ahead.PlannedClose, ahead.Projected)
	}
	if ahead.PlannedClose <= ahead.Target {
		t.Errorf("PlannedClose = %d, want above the goal (%d) — the rhythm alone clears it",
			ahead.PlannedClose, ahead.Target)
	}

	// A day already traded past its own average must not pull the close down:
	// Projected counts what the till holds, and money banked is never undone by
	// a plan.
	traded := buildProjection(rates,
		GoalProgress{RevenueTarget: 4000000, RevenueActual: 3900000, DaysTotal: 31, DaysRemaining: 9},
		monthClock, 500000)
	if traded.PlannedClose < traded.Projected {
		t.Errorf("PlannedClose = %d, want at least Projected (%d) on a day that outsold its average",
			traded.PlannedClose, traded.Projected)
	}

	// A month with no plan has nothing to add: the two figures are one.
	noGoal := buildProjection(rates, GoalProgress{DaysTotal: 31, DaysRemaining: 9}, monthClock, 0)
	if noGoal.PlannedClose != noGoal.Projected {
		t.Errorf("PlannedClose = %d, want Projected (%d) with no plan to meet",
			noGoal.PlannedClose, noGoal.Projected)
	}
}

// A future day's absences: the same reasons today's has, plus the ones only a
// date outside the month can hit.
func TestDayTargetNamesWhyAFutureDayHasNoAsk(t *testing.T) {
	flat := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 250000, DaysTotal: 31, DaysRemaining: 5}

	tests := []struct {
		name       string
		rates      dailyRates
		goals      GoalProgress
		month, now string
		day        int
		want       DayTargetState
	}{
		{"an ordinary day still ahead", flat, goals, "2026-07", "2026-07-27", 28, DayTargetOK},
		// Past the month's last day: it belongs to a month with its own goal and
		// its own gap, and this analysis has seen neither. Saying "sem_meta"
		// would be a claim about next month; this says where the question lands.
		{"a day past the month's last", flat, goals, "2026-07", "2026-07-27", 32, DayTargetMonthOver},
		// Tuesday 2026-07-28 priced at nothing while the rest of the week
		// trades: tomorrow is the closed door, today is not.
		{
			"a weekday the pharmacy does not trade",
			ratesFor(100000, 100000, 0, 100000, 100000, 100000, 100000),
			goals, "2026-07", "2026-07-27", 28, DayTargetClosedWeekday,
		},
		{"no window to price the days from", dailyRates{}, goals, "2026-07", "2026-07-27", 28, DayTargetNoHistory},
		// A goal already met is not here for the same reason it is not in
		// TestTodayTargetNamesWhyThereIsNoAsk: it is an ask at the weekday's own
		// rhythm now, and every day ahead gets one (ADR-025).
		{
			"no goal to take a share of", flat,
			GoalProgress{RevenueActual: 250000, DaysTotal: 31, DaysRemaining: 5},
			"2026-07", "2026-07-27", 28, DayTargetNoGoal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			monthClock := clock(t, tc.month, tc.now)
			got := buildProjection(tc.rates, tc.goals, monthClock, 0).Plan.at(tc.rates, monthClock, tc.day, 0)

			if got.State != tc.want {
				t.Errorf("State = %q, want %q", got.State, tc.want)
			}
			if !got.Asked() && (got.Target != 0 || got.Historical != 0) {
				t.Errorf("day = %+v, want no amounts under state %q", got, got.State)
			}
		})
	}

	// A day with no ask still names itself where there is a day to name, so the
	// bot can say *which* Tuesday it does not open on. A date outside the month
	// carries no date rather than a made-up one.
	monthClock := clock(t, "2026-07", "2026-07-27")
	closedTuesday := buildProjection(ratesFor(100000, 100000, 0, 100000, 100000, 100000, 100000),
		goals, monthClock, 0).Plan.at(ratesFor(100000, 100000, 0, 100000, 100000, 100000, 100000), monthClock, 28, 0)
	if closedTuesday.Date != "2026-07-28" || closedTuesday.Day != time.Tuesday {
		t.Errorf("day = %+v, want the day named even with no ask", closedTuesday)
	}
	if over := buildProjection(flat, goals, monthClock, 0).Plan.at(flat, monthClock, 32, 0); over.Date != "" {
		t.Errorf("Date = %q, want no date for a day outside the analysed month", over.Date)
	}
}

// The ask is a whole-day figure measured from the morning, so closing the day
// needs what the day actually took. It used to be nowhere in the analysis: the
// only faturamento a consumer could reach was the month's, today included.
func TestTodayTargetCarriesWhatTheDayHasSold(t *testing.T) {
	flat := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	// R$8.500,00 of a R$10.000,00 goal already in, R$1.200,00 of it sold today.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 850000, DaysTotal: 31, DaysRemaining: 5}

	got := buildProjection(flat, goals, clock(t, "2026-07", "2026-07-27"), 120000).TodayTarget

	if got.Realized != 120000 {
		t.Errorf("Realized = %d, want what the day has taken", got.Realized)
	}
	if got.Basis != DayInProgress {
		t.Errorf("Basis = %q, want %q for the day being traded", got.Basis, DayInProgress)
	}
	// Comparable to the ask without any further arithmetic: both cover the whole
	// day, and this one already clears it.
	if got.Realized <= got.Target {
		t.Errorf("Realized %d vs Target %d — the fixture no longer tests a day that beat its ask",
			got.Realized, got.Target)
	}
}

func TestProjectionWithoutAGoalHasNothingToPace(t *testing.T) {
	mondayOnly := ratesFor(0, 100000, 0, 0, 0, 0, 0)

	got := buildProjection(mondayOnly, GoalProgress{RevenueActual: 50000, DaysRemaining: 6}, clock(t, "2026-07", "2026-07-26"), 0)

	if got.Pacing() {
		t.Error("Pacing = true, want false with no target")
	}
	if got.Gap != 0 || got.OnTrack {
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
	// Saturday appears in both months, so its rate is the Gaussian-weighted
	// average of the two: August's is the window's most recent week and carries
	// full weight, July's is three weeks older and carries about a third, so the
	// rate leans towards the recent 120000 rather than sitting flat at 110000.
	if want := int64(115098); rates.avg[6] != want {
		t.Errorf("Saturday = %d, want both Saturdays weighted (%d)", rates.avg[6], want)
	}
	if rates.basis() != ProjectionFromWindow {
		t.Errorf("basis = %q, want an ordinary projection", rates.basis())
	}
}

// The month is projected at exactly the rates the weekday card displays: add up
// what the card says each day still to come is worth and you get the projection.
//
// The card used to aggregate the window a second time to draw itself, and only
// that second pass was Gaussian-weighted — the page showed one Tuesday average
// and projected the month off another. The card is now a view of the rates
// rather than a rival calculation, so this walks the whole way round instead:
// card → the days August has left → buildProjection's own arithmetic.
func TestTheMonthIsProjectedAtTheRatesTheCardShows(t *testing.T) {
	var window []domain.FinancialEntry
	// Eight weeks of trading, every weekday, at amounts that differ week to week
	// so a flat average and a weighted one cannot coincide by accident.
	for week := 0; week < 8; week++ {
		monday := day(t, "2026-06-08").Time().AddDate(0, 0, week*7)
		for d := 0; d < 7; d++ {
			date := domain.NewCalendarDate(monday.AddDate(0, 0, d))
			window = append(window, sale(t, date.String(), int64(100000+week*7000+d*3000)))
		}
	}

	now := at12(t, "2026-08-03")
	monthClock := clock(t, "2026-08", "2026-08-03")
	from, to := monthClock.projectionWindow()
	rates := projectionRates(window, from, to)
	card := rates.weekdayStats(now)

	// What the card promises for the 3rd through the 31st, read off the card
	// itself by looking up each remaining date's weekday row.
	var wantRemaining int64
	for d := monthClock.today; d <= monthClock.total; d++ {
		date := time.Date(2026, time.August, d, 12, 0, 0, 0, now.Location())
		wantRemaining += card[int(date.Weekday())].Avg
	}

	got := buildProjection(rates, GoalProgress{RevenueActual: 500000, DaysRemaining: monthClock.remaining}, monthClock, 0)

	if got.Remaining != wantRemaining {
		t.Errorf("projection prices the rest of the month at %d, but the card's own rows add up to %d",
			got.Remaining, wantRemaining)
	}
	if want := int64(500000) + wantRemaining; got.Projected != want {
		t.Errorf("Projected = %d, want faturamento so far plus the card's remaining days (%d)", got.Projected, want)
	}
}

// Actual + Remaining = Projected is the identity the card is read through:
// "faturado até agora" above "estimativa restante" above "projeção", and a
// reader who adds the first two must land on the third. It is asserted across
// every shape the projection comes in — not only the ordinary mid-month one —
// because the shapes are exactly where the three figures could drift apart: a
// closed month adds nothing, a day already traded past its average adds nothing
// either, and neither may leave Projected quoting a sum that was never taken.
func TestProjectionIsAlwaysActualPlusRemaining(t *testing.T) {
	week := ratesFor(60000, 122400, 119100, 102900, 121400, 121100, 148100)
	inAugust := GoalProgress{RevenueTarget: 4000000, RevenueActual: 358837, DaysTotal: 31, DaysRemaining: 28}

	tests := []struct {
		name         string
		rates        dailyRates
		goals        GoalProgress
		month, now   string
		todayRevenue int64
	}{
		{"mid-month", week, inAugust, "2026-08", "2026-08-04", 0},
		{"today already part traded", week, inAugust, "2026-08", "2026-08-04", 50000},
		{"today already past its average", week, inAugust, "2026-08", "2026-08-04", 500000},
		{"the first day of the month", week, GoalProgress{RevenueTarget: 4000000, DaysTotal: 31, DaysRemaining: 31}, "2026-08", "2026-08-01", 0},
		{"the last day of the month", week, GoalProgress{RevenueTarget: 1000000, RevenueActual: 900000, DaysTotal: 31, DaysRemaining: 1}, "2026-07", "2026-07-31", 0},
		{"a closed month", week, GoalProgress{RevenueTarget: 1000000, RevenueActual: 900000, DaysTotal: 31}, "2026-07", "2026-08-04", 0},
		{"no window to price the days from", dailyRates{}, inAugust, "2026-08", "2026-08-04", 0},
		{"no target to pace against", week, GoalProgress{RevenueActual: 358837, DaysTotal: 31, DaysRemaining: 28}, "2026-08", "2026-08-04", 0},
		{"a target already beaten", week, GoalProgress{RevenueTarget: 1000000, RevenueActual: 4000000, DaysTotal: 31, DaysRemaining: 28}, "2026-08", "2026-08-04", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildProjection(tc.rates, tc.goals, clock(t, tc.month, tc.now), tc.todayRevenue)

			if got.Actual != tc.goals.RevenueActual {
				t.Errorf("Actual = %d, want the faturamento already booked (%d)", got.Actual, tc.goals.RevenueActual)
			}
			// Never negative: a day that has beaten its average has nothing left
			// to bring, and must not be allowed to eat into what has been sold.
			if got.Remaining < 0 {
				t.Errorf("Remaining = %d, want nothing left rather than a negative estimate", got.Remaining)
			}
			if want := got.Actual + got.Remaining; got.Projected != want {
				t.Errorf("Projected = %d, want Actual + Remaining (%d + %d = %d)",
					got.Projected, got.Actual, got.Remaining, want)
			}
		})
	}
}

// On 4 August 2026 the month has 28 days left — exactly four Sundays, four
// Mondays and so on — so what is still to come can be checked in closed form:
// four times the sum of the seven weekday averages. buildProjection never does
// that arithmetic; it walks the calendar a day at a time. The two agreeing is
// what ties the estimate on the card to the averages printed right beside it,
// which is otherwise only ever checked by adding the card up by hand.
func TestTheRestOfTheMonthIsPricedAtItsRemainingWeekdays(t *testing.T) {
	monthClock := clock(t, "2026-08", "2026-08-04")
	if want := 4 * daysInWeek; monthClock.remaining != want {
		t.Fatalf("August has %d days left from the 4th, want %d — four of every weekday", monthClock.remaining, want)
	}

	// Every day of the window traded, at amounts that differ from week to week
	// so a Gaussian-weighted average and a flat one cannot coincide by accident.
	from, to := monthClock.projectionWindow()
	var window []domain.FinancialEntry
	for d := from.Time(); !d.After(to.Time()); d = d.AddDate(0, 0, 1) {
		window = append(window, sale(t, domain.NewCalendarDate(d).String(), int64(100000+d.YearDay()*137)))
	}
	rates := projectionRates(window, from, to)

	var perWeek int64
	for _, avg := range rates.avg {
		perWeek += avg
	}

	goals := GoalProgress{RevenueTarget: 4000000, RevenueActual: 358837, DaysTotal: 31, DaysRemaining: monthClock.remaining}
	got := buildProjection(rates, goals, monthClock, 0)

	if want := 4 * perWeek; got.Remaining != want {
		t.Errorf("Remaining = %d, want four of every weekday average (%d)", got.Remaining, want)
	}
	if want := goals.RevenueActual + got.Remaining; got.Projected != want {
		t.Errorf("Projected = %d, want faturamento so far plus the days left (%d)", got.Projected, want)
	}

	// The one thing the closed form has to bend for is today. What has already
	// sold on the 4th sits in Actual, so only the rest of an ordinary Tuesday is
	// still ahead — the sum drops by exactly the morning's takings, not by a
	// whole day.
	const soldToday = 20000
	if tuesday := rates.avg[int(time.Tuesday)]; tuesday <= soldToday {
		t.Fatalf("Tuesday averages %d, want more than the %d sold today so the day is netted rather than clamped", tuesday, soldToday)
	}
	partial := buildProjection(rates, goals, monthClock, soldToday)
	if want := 4*perWeek - soldToday; partial.Remaining != want {
		t.Errorf("Remaining = %d with %d already sold today, want %d", partial.Remaining, int64(soldToday), want)
	}
}

// A weighted projection follows the pharmacy when its trading level actually
// shifts. Under a flat average a doubling in the last fortnight was diluted by
// six older weeks and the projection took the full window to catch up.
func TestProjectionRatesFollowARecentShift(t *testing.T) {
	var window []domain.FinancialEntry
	for week := 0; week < 8; week++ {
		monday := day(t, "2026-06-08").Time().AddDate(0, 0, week*7)
		amount := int64(100000)
		if week >= 6 { // the last two weeks of the window
			amount = 200000
		}
		window = append(window, sale(t, domain.NewCalendarDate(monday).String(), amount))
	}

	from, to := clock(t, "2026-08", "2026-08-03").projectionWindow()
	monday := projectionRates(window, from, to).avg[1]

	// The flat average of six 100000s and two 200000s is 125000. Weighted, the
	// two recent Mondays dominate and the rate lands well above it.
	if monday <= 125000 {
		t.Errorf("Monday = %d, want more than the flat average (125000) — the recent weeks weigh more", monday)
	}
	if monday > 200000 {
		t.Errorf("Monday = %d, want no more than the recent weeks themselves (200000)", monday)
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
	if rates.sample() != 1 {
		t.Errorf("sample = %d, want the single day inside the window", rates.sample())
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

	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-08-03"))

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

	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-15"))

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
	// Faturamento and entradas de caixa come off the summary; despesa and
	// resultado are re-totalled over the days that have arrived, today included.
	// Every entry here falls on or before the 15th, so despesa is the whole
	// 300.000 and resultado is the four sales minus it — 549.999 in, not the
	// summary's hand-built 450.000, which is deliberately a different fixture.
	if got.KPIs.Faturamento != 450000 || got.KPIs.Despesa != 300000 || got.KPIs.Resultado != 249999 {
		t.Errorf("KPIs = %+v, want faturamento off the summary and the result over the days so far", got.KPIs)
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
	// Goals.RevenueActual is now through-today (not the full-month summary),
	// matching the projection's actual baseline. All four sales fall on or
	// before the 15th (today), so the total is 200k+100k+150k+99.999 = 549.999.
	if got.Goals.RevenueActual != 549999 {
		t.Errorf("Goals.RevenueActual = %d, want revenue through today (549999)", got.Goals.RevenueActual)
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

// The analysis page shows the per-day ask in the health insight, on the
// projection card, and the bot reads it back via the tool payload. The
// recommendation is now coverage-based and does not repeat the per-day ask —
// it interprets the projection as a verdict instead.
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

	if got.Projection.AccelerationPct() <= 0 {
		t.Fatalf("Projection = %+v, want a non-zero acceleration pct", got.Projection)
	}
	pct := got.Projection.AccelerationPct()

	var insight string
	for _, m := range got.Health.Messages {
		if m.Type == InsightGoalBehind {
			insight = m.Description
		}
	}
	if insight != "A projeção indica fechamento abaixo da meta." {
		t.Errorf("health insight = %q, want neutral projection message", insight)
	}
	if want := pct; got.ToolPayload()["aceleracao_necessaria_pct"] != want {
		t.Errorf("tool payload acceleration pct = %v, want %v", got.ToolPayload()["aceleracao_necessaria_pct"], want)
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
			Target: 1000000, Actual: 800000, DaysRemaining: 10,
		},
		Health: Health{
			Status: HealthAtencao,
			Messages: []Insight{
				{Severity: SeverityInfo, Title: "Resultado positivo", Description: "tudo certo"},
				{Severity: SeverityWarning, Title: "Receitas cairam", Description: "17% abaixo do mês passado"},
			},
		},
		Recommendations: []Recommendation{
			// recommendations[0] is the projection verdict, and is rendered.
			{Title: "Projeção abaixo da meta", Message: "O ritmo atual deve fechar o mês em torno de R$ 9.000,00."},
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

	// The one thing to do about it is the recommendation.
	ahead := analysis.AheadLines()
	wantAhead := []string{
		"Projeção abaixo da meta: O ritmo atual deve fechar o mês em torno de R$ 9.000,00.",
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

// The digest states the day's ask itself, against what that weekday brings.
// It used to carry no figure at all, which left the humanizer in the notifier
// deriving one — and the only arithmetic it had was the gap over the days left
// (ADR-019).
func TestDigestStatesTheDaysAskAgainstItsOwnWeekday(t *testing.T) {
	analysis := Analysis{
		Period: Period{ThroughDay: 8, DaysRemaining: 23, DaysTotal: 31, InProgress: true},
		Health: Health{Status: HealthBoa},
		Projection: Projection{
			TodayTarget: DayTarget{
				State:        DayTargetOK,
				Day:          time.Sunday,
				Historical:   60000,
				Target:       66000,
				Delta:        6000,
				DeltaPercent: 0.1,
				Status:       PaceAbove,
				Source:       TargetFromGap,
			},
		},
	}

	ahead := analysis.AheadLines()
	if len(ahead) == 0 {
		t.Fatal("AheadLines is empty — the day's ask is the actionable half of the digest")
	}
	want := "Meta de hoje (domingo): R$ 660,00 — 10% acima do que um domingo costuma faturar (R$ 600,00)."
	if ahead[0] != want {
		t.Errorf("ahead[0] = %q, want %q", ahead[0], want)
	}

	// The floored asks are worded as the rhythm they are, never as a lighter
	// day: the line the pharmacy reads at seven in the morning must not sound
	// like permission to sell less than an ordinary Sunday (ADR-025).
	floored := analysis
	floored.Projection.TodayTarget = DayTarget{
		State: DayTargetOK, Day: time.Sunday, Historical: 60000, Target: 60000, Status: PaceOnTrack,
		Source: TargetFromAverage,
	}
	wantFloor := "Meta de hoje (domingo): R$ 600,00 — o que um domingo costuma faturar; o mês está no ritmo."
	if got := floored.AheadLines()[0]; got != wantFloor {
		t.Errorf("floored ask = %q, want %q", got, wantFloor)
	}

	met := floored
	met.Projection.TodayTarget.Source = TargetGoalMet
	wantMet := "Meta de hoje (domingo): R$ 600,00 — a meta do mês já foi batida, então a de hoje é manter o que um domingo costuma faturar."
	if got := met.AheadLines()[0]; got != wantMet {
		t.Errorf("goal-met ask = %q, want %q", got, wantMet)
	}

	// No ask, no line — and never a line built from a state that has no
	// amounts behind it.
	for _, state := range []DayTargetState{DayTargetClosedWeekday, DayTargetNoHistory} {
		quiet := analysis
		quiet.Projection.TodayTarget = DayTarget{State: state, Day: time.Sunday}
		for _, line := range quiet.AheadLines() {
			if strings.Contains(line, "Meta de hoje") {
				t.Errorf("state %q produced %q, want no ask stated", state, line)
			}
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
		Projection: Projection{Target: 3100000, Actual: 0, DaysRemaining: 31},
		Recommendations: []Recommendation{
			{Title: "Projeção muito abaixo da meta", Message: "No ritmo atual o mês deve fechar em torno de R$ 0,00."},
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
	if len(ahead) != 1 {
		t.Errorf("AheadLines = %v, want one recommendation", ahead)
	}
	if !strings.Contains(ahead[0], "Projeção muito abaixo da meta") {
		t.Errorf("AheadLines = %v, want the recommendation", ahead)
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
	// Absent, not null: the digest serializes this payload to JSON for the model
	// (see DigestPayload), and a null there reads as a value — "zero days left".
	caixa := payload["caixa"].(map[string]any)
	if _, ok := caixa["dias_ate_saldo_negativo"]; ok {
		t.Errorf("dias_ate_saldo_negativo = %v, want the key absent when the balance never goes negative", caixa["dias_ate_saldo_negativo"])
	}
}

// TestDigestPayloadIsTheAnalysisMinusItsToolAffordances: the daily digest writes
// from the same insights JSON the bot reads, less the keys that only mean
// something to a caller that can make another call.
func TestDigestPayloadIsTheAnalysisMinusItsToolAffordances(t *testing.T) {
	analysis := Analysis{
		Month: "2026-07",
		KPIs:  KPIs{Faturamento: 123456, Despesa: 20000},
		CashPosition: CashPosition{
			CurrentBalance: 500000,
			Commitments:    CommitmentsCovered,
		},
	}

	payload := analysis.DigestPayload()

	// The figures are the same ones, unchanged.
	if payload["faturamento"] != 1234.56 {
		t.Errorf("faturamento = %v, want the same reais the bot gets", payload["faturamento"])
	}
	caixa, ok := payload["caixa"].(map[string]any)
	if !ok {
		t.Fatalf("caixa missing from the digest payload: %v", payload)
	}
	if caixa["compromissos_situacao"] != string(CommitmentsCovered) {
		t.Errorf("compromissos_situacao = %v", caixa["compromissos_situacao"])
	}

	for _, key := range toolOnlyPayloadKeys {
		if _, present := payload[key]; present {
			t.Errorf("digest payload carries %q, which only a caller with tools can act on", key)
		}
	}
	// And ToolPayload keeps them — this is a subtraction, not a move.
	for _, key := range toolOnlyPayloadKeys {
		if _, present := analysis.ToolPayload()[key]; !present {
			t.Errorf("ToolPayload lost %q", key)
		}
	}
}

// TestDigestPayloadTellsTheWriterToDoNothingItCannotDo checks the payload's
// *content*, not its key list, and that distinction is the whole test.
//
// Iterating toolOnlyPayloadKeys — as the test above does — can only confirm that
// the keys someone remembered to list are gone. It passes just as happily when a
// new instruction is added to ToolPayload under a key nobody added to the list,
// which is exactly how "Peça a seção despesas_completas" reached a WhatsApp
// message whose reader has no seções to ask for.
func TestDigestPayloadTellsTheWriterToDoNothingItCannotDo(t *testing.T) {
	// Six categories, so the ranking is cut and the truncation warning fires.
	composition := make([]CategoryComposition, 0, maxToolCategories+1)
	for i := range maxToolCategories + 1 {
		composition = append(composition, CategoryComposition{
			CategoryID:   fmt.Sprintf("cat-%d", i),
			CategoryName: fmt.Sprintf("Categoria %d", i),
			Amount:       int64(1000 * (i + 1)),
			Percentage:   10,
		})
	}
	analysis := Analysis{Month: "2026-07", ExpenseComposition: composition}

	encoded, err := json.Marshal(analysis.DigestPayload())
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)

	// Anything that asks the reader — or the writer — to make another call.
	for _, forbidden := range []string{"Peça", "peça", "chame", "seção", "secoes", dayTargetToolName} {
		if strings.Contains(payload, forbidden) {
			t.Errorf("digest payload contains %q, an instruction a one-shot writer cannot follow:\n%s", forbidden, payload)
		}
	}
	// The cut itself still has to be announced: five of six categories presented
	// as the whole ranking is what ADR-015 exists to prevent.
	if !strings.Contains(payload, "maiores_despesas_warning") {
		t.Errorf("digest payload drops the truncation warning entirely:\n%s", payload)
	}
	if !strings.Contains(payload, "Mostrando as 5 maiores de 6 categorias") {
		t.Errorf("digest payload does not say how much of the ranking it is showing:\n%s", payload)
	}
}

// The whole point of ADR-019: the model is handed the weekday-scaled ask, so it
// never has to derive a per-day figure from the gap and the days left.
func TestToolPayloadCarriesTheDaysAskAndItsHistory(t *testing.T) {
	// Eight weeks where a Sunday is worth a third of a Saturday, which is the
	// shape a flat daily average gets wrong.
	var window []domain.FinancialEntry
	for week := 0; week < projectionWindowWeeks; week++ {
		monday := day(t, "2026-06-08").Time().AddDate(0, 0, week*7)
		for d := 0; d < daysInWeek; d++ {
			date := domain.NewCalendarDate(monday.AddDate(0, 0, d))
			amount := int64(120000)
			if date.Time().Weekday() == time.Sunday {
				amount = 40000
			}
			window = append(window, sale(t, date.String(), amount))
		}
	}
	// RevenueEntries must be month-scoped so revenueThroughDay (which matches
	// by day-of-month) doesn't count June/July entries.
	monthRevenue := []domain.FinancialEntry{
		sale(t, "2026-08-01", 120000),
		sale(t, "2026-08-02", 120000),
		sale(t, "2026-08-03", 40000), // Sunday
		sale(t, "2026-08-04", 120000),
		sale(t, "2026-08-05", 120000),
		sale(t, "2026-08-06", 120000),
		sale(t, "2026-08-07", 120000),
		sale(t, "2026-08-08", 120000),
		sale(t, "2026-08-09", 120000),
	}

	analysis := Build(Input{
		Month:                "2026-08",
		Entries:              window,
		RevenueEntries:       monthRevenue,
		WindowRevenueEntries: window,
		Summaries:            []*pkgfinance.MonthlySummary{nil, nil, summary(360000, 0)},
		Goals:                []*domain.Goal{nil, nil, {Month: "2026-08", RevenueTarget: 10000000}},
		Now:                  at12(t, "2026-08-09"), // a Sunday
	})

	meta, ok := analysis.ToolPayload()["meta_de_hoje"].(map[string]any)
	if !ok {
		t.Fatalf("meta_de_hoje missing from the payload — the model is left to divide the gap by the days left")
	}
	if meta["situacao"] != string(DayTargetOK) {
		t.Fatalf("meta_de_hoje = %v, want a real ask on a Sunday the pharmacy trades", meta)
	}
	if meta["dia_da_semana"] != "domingo" {
		t.Errorf("dia_da_semana = %v, want the weekday named for a model that reads it aloud", meta["dia_da_semana"])
	}
	// The ask is quoted beside what a Sunday actually brings. Without it
	// "R$ 500,00" is unreadable — the whole failure this replaced.
	hist, isFloat := meta["media_historica"].(float64)
	if !isFloat || hist <= 0 {
		t.Fatalf("media_historica = %v, want the Sunday average beside the ask", meta["media_historica"])
	}
	// Sundays are a third of the other days here, so the ask has to be far under
	// a flat share of the gap — that is the entire reason this field exists.
	if flat := reais(analysis.Projection.Gap) / float64(analysis.Period.DaysRemaining); hist >= flat {
		t.Errorf("Sunday average %v is not below the flat per-day share %v — the fixture no longer tests anything", hist, flat)
	}
	if ask := meta["meta"].(float64); ask <= 0 || ask > 4*hist {
		t.Errorf("meta = %v, want an ask scaled to a Sunday (média %v), not to the calendar", ask, hist)
	}
}

// The question this was asked at the end of a trading day: "já registrei o
// faturamento de hoje, como estamos para amanhã?". It was answered with a
// meta_de_amanha field, which is per-day and does not survive "e no sábado?".
// The payload now names the tool that prices any day — ADR-021.
func TestToolPayloadPointsAtTheDayTool(t *testing.T) {
	august := []domain.FinancialEntry{
		sale(t, "2026-08-01", 150000),
		sale(t, "2026-08-04", 160000),
	}

	analysis := Build(Input{
		Month:                "2026-08",
		Entries:              august,
		RevenueEntries:       august,
		WindowRevenueEntries: august,
		Summaries:            []*pkgfinance.MonthlySummary{nil, nil, summary(310000, 0)},
		Goals:                []*domain.Goal{nil, nil, {Month: "2026-08", RevenueTarget: 4500000}},
		Now:                  at12(t, "2026-08-04"),
	})
	payload := analysis.ToolPayload()

	if _, has := payload["meta_de_amanha"]; has {
		t.Error("meta_de_amanha is back — a field per day is not an answer to \"e no sábado?\"")
	}
	pointer, _ := payload["meta_de_outro_dia"].(string)
	if !strings.Contains(pointer, dayTargetToolName) {
		t.Errorf("meta_de_outro_dia = %q, want the tool named so the model does not conclude today is all there is", pointer)
	}

	// Today still arrives unasked (ADR-019), now saying which regime it is in
	// and what the day has taken so far — the two halves of closing a day.
	hoje := payload["meta_de_hoje"].(map[string]any)
	if hoje["data"] != "2026-08-04" || hoje["apuracao"] != string(DayInProgress) {
		t.Errorf("meta_de_hoje = %v, want today named and marked as being traded", hoje)
	}
	if hoje["realizado"] != 1600.0 {
		t.Errorf("realizado = %v, want today's own takings in reais", hoje["realizado"])
	}
	if _, has := payload["faturamento_de_hoje"]; has {
		t.Error("faturamento_de_hoje survived — it belongs on the day it describes")
	}
}

// The three regimes, over one seeded ledger: what a day did, what today is
// being asked for, what a day ahead is being asked for. The distinction is the
// point — the same three numbers read as a fact or as a bet.
func TestDayTargetsPriceEachRegime(t *testing.T) {
	ctx := context.Background()
	store := pkgfinance.NewInMemoryStore()

	// Eight weeks where a Wednesday is the quietest weekday, so a Wednesday's
	// ask cannot be mistaken for a flat share of the gap.
	var seeded []domain.FinancialEntry
	for week := range projectionWindowWeeks {
		monday := day(t, "2026-06-08").Time().AddDate(0, 0, week*7)
		for d := range daysInWeek {
			date := domain.NewCalendarDate(monday.AddDate(0, 0, d))
			amount := int64(150000)
			if date.Time().Weekday() == time.Wednesday {
				amount = 100000
			}
			seeded = append(seeded, sale(t, date.String(), amount))
		}
	}
	seeded = append(
		seeded,
		sale(t, "2026-08-03", 150000), // Monday, closed
		sale(t, "2026-08-04", 160000), // Tuesday, today
	)
	seed(t, store, seeded...)
	if err := store.SaveGoal(ctx, domain.Goal{UserID: "u1", Month: "2026-08", RevenueTarget: 4500000}); err != nil {
		t.Fatalf("save goal: %v", err)
	}

	now := at12(t, "2026-08-04")
	got, err := dayTargets(ctx, store, "u1", []domain.CalendarDate{
		day(t, "2026-08-03"), day(t, "2026-08-04"), day(t, "2026-08-05"),
	}, now)
	if err != nil {
		t.Fatalf("dayTargets: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d days, want 3", len(got))
	}
	ontem, hoje, amanha := got[0], got[1], got[2]

	// Yesterday: a fact. It reports what it sold and carries no target at all —
	// charging today's plan to a day nobody can sell on any more would be a goal
	// invented backwards.
	if ontem["apuracao"] != string(DayRealized) || ontem["situacao"] != string(DayTargetClosedDay) {
		t.Errorf("ontem = %v, want a closed day", ontem)
	}
	if ontem["realizado"] != 1500.0 {
		t.Errorf("ontem realizado = %v, want what the Monday sold", ontem["realizado"])
	}
	if _, has := ontem["meta"]; has {
		t.Errorf("ontem = %v, want no target on a day that has finished", ontem)
	}

	// Today: both halves, and they are directly comparable.
	if hoje["apuracao"] != string(DayInProgress) || hoje["realizado"] != 1600.0 {
		t.Errorf("hoje = %v, want the day being traded with its takings", hoje)
	}
	if _, has := hoje["meta"]; !has {
		t.Errorf("hoje = %v, want the ask that still stands", hoje)
	}

	// Tomorrow: a Wednesday, priced at its own rhythm and marked as a bet.
	if amanha["apuracao"] != string(DayProjected) || amanha["data"] != "2026-08-05" {
		t.Errorf("amanha = %v, want a projected Wednesday", amanha)
	}
	if amanha["dia_da_semana"] != "quarta" {
		t.Errorf("amanha = %v, want the weekday named for a model that reads it aloud", amanha)
	}
	// A day still ahead has sold nothing, and a "realizado: 0" would read as a
	// day that took nothing rather than one that has not begun.
	if _, has := amanha["realizado"]; has {
		t.Errorf("amanha = %v, want no realizado on a day that has not started", amanha)
	}
	hist, _ := amanha["media_historica"].(float64)
	if hist != 1000 {
		t.Fatalf("media_historica = %v, want a Wednesday's own average", amanha["media_historica"])
	}
	if ask, _ := amanha["meta"].(float64); ask <= hist {
		t.Errorf("meta = %v, want a Wednesday behind its goal asked for more than its usual %v", ask, hist)
	}
}

// A day of a month that has already closed. Every day of it is in the past, so
// it answers the same way yesterday does — the analysis of a closed month has
// no "today" to measure against, and the weekday averages for it come from the
// window ending on that month's own last day.
func TestDayTargetsAnswerForAClosedMonth(t *testing.T) {
	ctx := context.Background()
	store := pkgfinance.NewInMemoryStore()

	var seeded []domain.FinancialEntry
	for i := range 60 {
		d := day(t, "2026-06-01").Time().AddDate(0, 0, i)
		seeded = append(seeded, sale(t, domain.NewCalendarDate(d).String(), 120000))
	}
	seed(t, store, seeded...)

	got, err := dayTargets(ctx, store, "u1",
		[]domain.CalendarDate{day(t, "2026-06-10")}, at12(t, "2026-08-04"))
	if err != nil {
		t.Fatalf("dayTargets: %v", err)
	}

	if got[0]["apuracao"] != string(DayRealized) || got[0]["situacao"] != string(DayTargetClosedDay) {
		t.Errorf("day = %v, want a closed day, not a closed month with nothing to say", got[0])
	}
	if got[0]["realizado"] != 1200.0 || got[0]["media_historica"] != 1200.0 {
		t.Errorf("day = %v, want what it sold beside what that weekday brought", got[0])
	}
	if _, has := got[0]["meta"]; has {
		t.Errorf("day = %v, want no target on a day two months gone", got[0])
	}
}

// A demand is not an achievement, and the payload has to say which. At ten past
// midnight on a Wednesday, with nothing sold, the bot read "meta R$ 1.149,68,
// média R$ 1.028,29, status above" as "estamos com um bom desempenho, superando
// a média histórica". The ask is above a usual Wednesday *because the month is
// behind*; the direction alone means opposite things either side of today.
func TestADayAskIsKeyedAsEffortAndAResultAsPerformance(t *testing.T) {
	rates := ratesFor(100000, 100000, 100000, 100000, 100000, 100000, 100000)
	// A month far enough behind that every remaining day is asked for more than
	// a usual one.
	goals := GoalProgress{RevenueTarget: 1000000, RevenueActual: 100000, DaysTotal: 31, DaysRemaining: 5}
	monthClock := clock(t, "2026-07", "2026-07-29")
	plan := buildProjection(rates, goals, monthClock, 0).Plan

	// Tomorrow: a day being asked to work harder than usual.
	ask := dayTargetToolPayload(plan.at(rates, monthClock, 30, 0))
	if ask["esforco"] != string(PaceAbove) || ask["meta_vs_media_pct"].(int) <= 0 {
		t.Fatalf("ask = %v, want the stretch keyed to the meta", ask)
	}
	for _, key := range []string{"desempenho", "realizado_vs_media_pct"} {
		if _, has := ask[key]; has {
			t.Errorf("ask = %v, want no %q on a day that has not been traded", ask, key)
		}
	}

	// Yesterday: a day that actually beat its usual. Same direction, opposite
	// meaning, and a different key so the two cannot be read as one.
	result := dayTargetToolPayload(plan.at(rates, monthClock, 28, 150000))
	if result["desempenho"] != string(PaceAbove) || result["realizado_vs_media_pct"].(int) != 50 {
		t.Fatalf("result = %v, want the performance keyed to what it sold", result)
	}
	for _, key := range []string{"esforco", "meta_vs_media_pct", "meta"} {
		if _, has := result[key]; has {
			t.Errorf("result = %v, want no %q on a day that has closed", result, key)
		}
	}
}

// A month that has not started is not a month that has ended, and monthClock
// cannot tell them apart — both report inProgress = false. Without this, "dia 3
// de setembro" asked in August answered "mes_fechado".
func TestDayTargetsNameAMonthThatHasNotStarted(t *testing.T) {
	ctx := context.Background()
	store := pkgfinance.NewInMemoryStore()

	got, err := dayTargets(ctx, store, "u1",
		[]domain.CalendarDate{day(t, "2026-09-03")}, at12(t, "2026-08-04"))
	if err != nil {
		t.Fatalf("dayTargets: %v", err)
	}
	if got[0]["situacao"] != string(DayTargetFutureMonth) {
		t.Errorf("situacao = %v, want %q — a month ahead has no gap to distribute yet",
			got[0]["situacao"], DayTargetFutureMonth)
	}
	// Still named: the model has to be able to say which day it is declining to
	// price.
	if got[0]["data"] != "2026-09-03" || got[0]["dia_da_semana"] != "quinta" {
		t.Errorf("day = %v, want the date named even with nothing to say about it", got[0])
	}
}

func TestParseDayTargetDates(t *testing.T) {
	now := at12(t, "2026-08-04")

	// No argument is a valid call: "quanto preciso vender?" means today.
	got, err := parseDayTargetDates(nil, now)
	if err != nil || len(got) != 1 || got[0].String() != "2026-08-04" {
		t.Errorf("empty = %v, %v, want today", got, err)
	}

	// The same day named twice is one day. Pricing it twice would read as two.
	got, err = parseDayTargetDates([]string{"2026-08-05", "2026-08-04", "2026-08-05"}, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 || got[0].String() != "2026-08-05" || got[1].String() != "2026-08-04" {
		t.Errorf("got %v, want the repeat dropped and the order kept", got)
	}

	// A malformed date is an error, never a silently skipped entry: "não achei
	// nada para essa data" and "você digitou errado" must not read the same.
	if _, err := parseDayTargetDates([]string{"05/08/2026"}, now); err == nil {
		t.Error("parse accepted a malformed date — a typo would come back as an empty answer")
	}
}

// A cut list says it was cut. A model that asked for ten days and silently got
// seven would present the seven as the whole week (ADR-015).
func TestDayTargetToolWarnsWhenItCuts(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	tool := dayTargetTool(store, time.UTC)

	dates := make([]string, 0, maxDayTargets+3)
	for i := range maxDayTargets + 3 {
		dates = append(dates, time.Now().UTC().AddDate(0, 0, i).Format("2006-01-02"))
	}
	args, err := json.Marshal(map[string]any{"datas": dates})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	raw, err := tool.Handler(context.Background(), "u1", args)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	got := raw.(map[string]any)

	if got["truncated"] != true {
		t.Errorf("truncated = %v, want true when more days were asked for than served", got["truncated"])
	}
	if w, _ := got["warning"].(string); !strings.Contains(w, "de 10 datas") {
		t.Errorf("warning = %q, want it to say how many were left out", w)
	}
	if days, _ := got["dias"].([]map[string]any); len(days) != maxDayTargets {
		t.Errorf("got %d days, want the cap of %d", len(days), maxDayTargets)
	}
}

// A cash question about tomorrow used to be answered with the month: a
// month-end balance and R$ 20.096,97 of commitments, both true, neither about
// the day the pharmacy is opening next.
func TestToolPayloadCarriesTomorrowsCash(t *testing.T) {
	due := day(t, "2026-07-11")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 500000),
		{
			TransactionDate: due,
			DueDate:         &due,
			Amount:          80000,
			Type:            domain.EntryTypeExpense,
			Category:        "fornecedor",
		},
	}
	analysis := Build(Input{
		Month:     "2026-07",
		Entries:   entries,
		Summaries: []*pkgfinance.MonthlySummary{nil, nil, summary(500000, 80000)},
		CashFlowPoints: []pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: 500000},
			{Date: "2026-07-11", ProjectedExpense: 80000, RunningBalance: 420000},
			{Date: "2026-07-31", RunningBalance: 420000},
		},
		Now: at12(t, "2026-07-10"),
	})

	caixa := analysis.ToolPayload()["caixa"].(map[string]any)
	amanha, ok := caixa["amanha"].(map[string]any)
	if !ok {
		t.Fatal("caixa.amanha missing — the only cash figures left are about the whole month")
	}
	if amanha["data"] != "2026-07-11" {
		t.Errorf("data = %v, want tomorrow", amanha["data"])
	}
	if amanha["despesas_agendadas"] != 800.0 {
		t.Errorf("despesas_agendadas = %v, want the day's own bills in reais", amanha["despesas_agendadas"])
	}
	if amanha["saldo_projetado"] != 4200.0 {
		t.Errorf("saldo_projetado = %v, want the balance the day ends on", amanha["saldo_projetado"])
	}

	// On the month's last day there is no tomorrow in the forecast, and the key
	// is absent rather than a row of zeroes a model would read aloud as a day
	// with nothing moving.
	closing := Build(Input{
		Month:          "2026-07",
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(500000, 0)},
		CashFlowPoints: []pkgfinance.CashFlowPoint{{Date: "2026-07-31", RunningBalance: 420000}},
		Now:            at12(t, "2026-07-31"),
	})
	if _, has := closing.ToolPayload()["caixa"].(map[string]any)["amanha"]; has {
		t.Error("caixa.amanha present on the month's last day, want it absent")
	}
}

// A commitment is a liquidity question, so it travels with the runway that
// answers it — not among the figures that describe how the month performed,
// where the amount alone read as a finding.
func TestToolPayloadPutsCommitmentsWithTheRunway(t *testing.T) {
	due := day(t, "2026-07-25")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 500000),
		{
			TransactionDate: due,
			DueDate:         &due,
			Amount:          80000,
			Type:            domain.EntryTypeExpense,
			Category:        "aluguel",
		},
	}

	analysis := Build(Input{
		Month:                "2026-07",
		Entries:              entries,
		RevenueEntries:       entries,
		WindowRevenueEntries: entries,
		WindowEntries:        entries,
		Summaries:            []*pkgfinance.MonthlySummary{nil, nil, summary(500000, 80000)},
		CashFlowPoints: []pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: 500000},
			{Date: "2026-07-25", ProjectedExpense: 80000, RunningBalance: 420000},
		},
		Now: at12(t, "2026-07-10"),
	})
	payload := analysis.ToolPayload()

	// Not beside despesa/resultado, which are about how the month is going. The
	// rent due on the 25th says nothing about that.
	if _, has := payload["despesa_agendada"]; has {
		t.Error("despesa_agendada is back at the top level, where a total reads as a verdict on the month")
	}

	caixa := payload["caixa"].(map[string]any)
	if caixa["compromissos_do_mes"] != 800.0 {
		t.Errorf("compromissos_do_mes = %v, want what is still to fall due", caixa["compromissos_do_mes"])
	}
	// And never the amount on its own: the question it raises is answered here.
	if caixa["compromissos_situacao"] != string(CommitmentsCovered) {
		t.Errorf("compromissos_situacao = %v, want %q on a runway that holds",
			caixa["compromissos_situacao"], CommitmentsCovered)
	}
}

// The curve is for the page, not for the chat: thirty-one days of series cost
// tokens on every question the bot asks, and the bot needs a day.
func TestToolPayloadLeavesTheCurveOut(t *testing.T) {
	analysis := Build(Input{
		Month:     "2026-07",
		Summaries: []*pkgfinance.MonthlySummary{nil, nil, summary(500000, 0)},
		CashFlowPoints: []pkgfinance.CashFlowPoint{
			{Date: "2026-07-10", RunningBalance: 500000},
			{Date: "2026-07-11", RunningBalance: 500000},
		},
		Now: at12(t, "2026-07-10"),
	})

	if len(analysis.CashPosition.Forecast) == 0 {
		t.Fatal("the analysis has no curve — the fixture no longer tests anything")
	}
	caixa := analysis.ToolPayload()["caixa"].(map[string]any)
	for _, key := range []string{"forecast", "curva", "serie"} {
		if _, has := caixa[key]; has {
			t.Errorf("caixa.%s is in the bot payload, want the series left to the page", key)
		}
	}
}

// An absence is never a bare silence: the model has to be able to say which of
// the reasons it is, or it fills the gap with arithmetic of its own.
func TestToolPayloadNamesWhyThereIsNoAskToday(t *testing.T) {
	analysis := Build(Input{
		Month:     "2026-07",
		Summaries: []*pkgfinance.MonthlySummary{nil, nil, summary(0, 0)},
		Now:       at12(t, "2026-07-15"),
	})

	meta, ok := analysis.ToolPayload()["meta_de_hoje"].(map[string]any)
	if !ok {
		t.Fatal("meta_de_hoje missing — an absent key reads as 'the analysis has no such thing'")
	}
	if meta["situacao"] != string(DayTargetNoGoal) {
		t.Errorf("situacao = %v, want %q for a month with no target", meta["situacao"], DayTargetNoGoal)
	}
	// No amounts under an absence: a "meta: 0" would be read aloud as a target
	// of nothing.
	if _, has := meta["meta"]; has {
		t.Errorf("meta_de_hoje = %v, want no amount when there is no ask", meta)
	}
}

// Everything the dashboard renders is reachable from a chat — but not carried
// in every answer. The base payload names what it left out, and the sections
// arrive when asked for.
func TestToolPayloadAdvertisesTheSectionsItLeftOut(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-01", 500000),
		expense(t, "2026-07-02", "aluguel", 300000),
		expense(t, "2026-07-03", "energia_agua", 100000),
	}
	analysis := Build(Input{
		Month:          "2026-07",
		Entries:        entries,
		RevenueEntries: entries,
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(500000, 400000)},
		Now:            at12(t, "2026-07-15"),
	})

	base := analysis.ToolPayload()
	index, ok := base["secoes_disponiveis"].([]map[string]any)
	if !ok || len(index) != len(sectionCatalog) {
		t.Fatalf("secoes_disponiveis = %v, want one entry per section", base["secoes_disponiveis"])
	}
	for _, s := range sectionCatalog {
		if _, sent := base[string(s.Name)]; sent {
			t.Errorf("%q rides along in the base payload — every answer pays for it", s.Name)
		}
	}

	// Asked for, it arrives, and the advertised name is the name that works.
	with := analysis.ToolPayload(AllSections()...)
	for _, s := range sectionCatalog {
		if _, sent := with[string(s.Name)]; !sent {
			t.Errorf("section %q was advertised but not served", s.Name)
		}
	}

	// A name the tool does not serve is dropped rather than failing the call:
	// the month is still answerable without it.
	if got := parseSections([]string{"nao_existe", string(SectionHistory)}); len(got) != 1 || got[0] != SectionHistory {
		t.Errorf("parseSections = %v, want the unknown name dropped and the real one kept", got)
	}
}

// A cut ranking that does not say it was cut describes a twelve-category month
// as a five-category one (ADR-015).
func TestToolPayloadWarnsWhenTheExpenseRankingIsCut(t *testing.T) {
	composition := make([]CategoryComposition, 0, maxToolCategories+3)
	for i := range cap(composition) {
		composition = append(composition, CategoryComposition{
			CategoryName: fmt.Sprintf("categoria %d", i),
			Amount:       int64(1000 * (cap(composition) - i)),
		})
	}

	full := Analysis{ExpenseComposition: composition}.ToolPayload()
	if full["maiores_despesas_truncado"] != true {
		t.Errorf("maiores_despesas_truncado = %v, want true with %d categories", full["maiores_despesas_truncado"], len(composition))
	}
	warning, _ := full["maiores_despesas_warning"].(string)
	if !strings.Contains(warning, string(SectionExpensesFull)) {
		t.Errorf("warning = %q, want it to name the section that serves the whole list", warning)
	}
	if shown := full["maiores_despesas"].([]map[string]any); len(shown) != maxToolCategories {
		t.Errorf("maiores_despesas has %d rows, want %d", len(shown), maxToolCategories)
	}

	// Nothing was cut, so nothing is warned about — a caveat that is always on
	// is not read at all.
	short := Analysis{ExpenseComposition: composition[:2]}.ToolPayload()
	if _, warned := short["maiores_despesas_truncado"]; warned {
		t.Error("a complete ranking must not carry a truncation warning")
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

	health := buildHealth(entries, compared, buildTrends(compared), WeekComparison{}, Projection{})
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
	rates := dailyRates{weeks: [daysInWeek]int{1, 1, 1, 1, 1, 1, 1}}
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
	rates := dailyRates{weeks: [daysInWeek]int{1, 1, 1, 1, 1, 1, 1}}
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
	// Both receipts land on a Friday, so the rate is their Gaussian-weighted
	// average: the 17th is two weeks nearer the end of the window than the 10th
	// and counts for more, so the figure sits above a flat 200000.
	if want := int64(230271); rates.avg[int(time.Friday)] != want {
		t.Errorf("Friday = %d, want both receipts weighted (%d)", rates.avg[int(time.Friday)], want)
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

	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-27")) // a Monday

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

	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-14")) // a Tuesday

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
	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-13"))

	monday := stats[1]
	// The recent outlier (500000 at offset 0, w=1.0) must pull the average
	// well above the old series (10000 at offsets 1–7).
	if monday.Avg < 50000 {
		t.Errorf("Avg = %d, want >> 50000 — recent outlier must dominate", monday.Avg)
	}
}

func TestWeekdayStatsWeightedBasisThresholds(t *testing.T) {
	// No data → sem_base.
	empty := projectionRates(nil, day(t, "2026-06-15"), day(t, "2026-07-14")).weekdayStats(at12(t, "2026-07-15"))
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
	fewStats := projectionRates(few, day(t, "2026-06-15"), day(t, "2026-07-14")).weekdayStats(at12(t, "2026-07-15"))
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
	manyStats := projectionRates(many, day(t, "2026-05-18"), day(t, "2026-07-14")).weekdayStats(at12(t, "2026-07-15"))
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
	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-15"))
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

	stats := projectionRates(entries, from, to).weekdayStats(at12(t, "2026-07-15"))
	monday := stats[1]
	if monday.Avg != 10000 {
		t.Errorf("Avg = %d, want 10000 (expense must not count)", monday.Avg)
	}
}

// A month in progress carries bills booked for days that have not arrived, and
// every retrospective figure has to stop where KPIs.Despesa stops. They did not:
// the KPI was re-totalled over the elapsed days while the goal card, the
// composition, the cash-out days and the highlights all still read the whole
// month. One tool payload then carried "despesa R$ 300,00" beside a composition
// totalling R$ 900,00 and a "pior dia" three days in the future — the R$ 600,00
// of difference being bills nobody had paid. What is booked for later is
// KPIs.DespesaAgendada, beside the runway that says whether there is money for
// it (ADR-022), and it belongs in no total of what the month has spent.
func TestRetrospectiveFiguresStopWhereDespesaStops(t *testing.T) {
	now := at12(t, "2026-07-15")
	entries := []domain.FinancialEntry{
		sale(t, "2026-07-14", 250000),
		sale(t, "2026-07-15", 180000), // today: traded, and it counts
		expense(t, "2026-07-05", "aluguel", 300000),
		// Booked for days still ahead: a commitment, not a spend.
		expense(t, "2026-07-20", "fornecedor", 400000),
		expense(t, "2026-07-28", "energia", 200000),
	}

	got := Build(Input{
		Month:          "2026-07",
		Entries:        entries,
		RevenueEntries: entries,
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(430000, 900000)},
		Goals:          []*domain.Goal{nil, nil, {RevenueTarget: 1000000, ExpenseTarget: 1000000}},
		Now:            now,
	})

	if got.KPIs.Despesa != 300000 {
		t.Fatalf("KPIs.Despesa = %d, want only the 300000 that has left the account", got.KPIs.Despesa)
	}
	if got.KPIs.DespesaAgendada != 600000 {
		t.Errorf("KPIs.DespesaAgendada = %d, want the 600000 still to fall due", got.KPIs.DespesaAgendada)
	}

	// The ceiling is spent against, not committed against.
	if got.Goals.ExpenseActual != got.KPIs.Despesa {
		t.Errorf("Goals.ExpenseActual = %d, want it to agree with KPIs.Despesa (%d)",
			got.Goals.ExpenseActual, got.KPIs.Despesa)
	}
	if got.Goals.ExpensePct != 30 {
		t.Errorf("Goals.ExpensePct = %d, want 30 — 300000 of a 1000000 ceiling", got.Goals.ExpensePct)
	}

	// A breakdown decomposes a total, so it covers the total's own days.
	var composed int64
	for _, c := range got.ExpenseComposition {
		composed += c.Amount
	}
	if composed != got.KPIs.Despesa {
		t.Errorf("ExpenseComposition totals %d, want it to decompose KPIs.Despesa (%d)",
			composed, got.KPIs.Despesa)
	}

	for _, d := range got.CashOutDays {
		if d.Date > "2026-07-15" {
			t.Errorf("CashOutDays names %s — money that has not left the account yet", d.Date)
		}
	}

	// The worst day of the month cannot be a day the month has not reached: it
	// has no sales because it has not happened, not because it went badly.
	if got.Highlights.WorstIncome.Date > "2026-07-15" {
		t.Errorf("Highlights.WorstIncome = %+v, want a day that has arrived", got.Highlights.WorstIncome)
	}
	if got.Highlights.WorstBalance.Date > "2026-07-15" {
		t.Errorf("Highlights.WorstBalance = %+v, want a day that has arrived", got.Highlights.WorstBalance)
	}

	// And the payload the bot reads carries one despesa, not three.
	payload := got.ToolPayload(SectionCashOutDays, SectionExpensesFull)
	if payload["despesa"] != reais(got.KPIs.Despesa) {
		t.Errorf("payload despesa = %v, want %v", payload["despesa"], reais(got.KPIs.Despesa))
	}
	meta := payload["meta"].(map[string]any)
	if meta["despesa_atual"] != payload["despesa"] {
		t.Errorf("meta.despesa_atual = %v, want the same despesa as the row above (%v)",
			meta["despesa_atual"], payload["despesa"])
	}
}

// A closed month has no days left to exclude, so nothing here narrows it: the
// filter is "days that have arrived", and in a finished month that is all of
// them.
func TestClosedMonthKeepsEveryDay(t *testing.T) {
	entries := []domain.FinancialEntry{
		sale(t, "2026-06-02", 100000),
		expense(t, "2026-06-10", "aluguel", 300000),
		expense(t, "2026-06-28", "fornecedor", 200000),
	}

	got := Build(Input{
		Month:          "2026-06",
		Entries:        entries,
		RevenueEntries: entries,
		Summaries:      []*pkgfinance.MonthlySummary{nil, nil, summary(100000, 500000)},
		Goals:          []*domain.Goal{nil, nil, {ExpenseTarget: 1000000}},
		Now:            at12(t, "2026-07-15"),
	})

	if got.KPIs.Despesa != 500000 {
		t.Errorf("KPIs.Despesa = %d, want the whole closed month (500000)", got.KPIs.Despesa)
	}
	if got.Goals.ExpenseActual != 500000 {
		t.Errorf("Goals.ExpenseActual = %d, want the whole closed month (500000)", got.Goals.ExpenseActual)
	}
	var composed int64
	for _, c := range got.ExpenseComposition {
		composed += c.Amount
	}
	if composed != 500000 {
		t.Errorf("ExpenseComposition totals %d, want the whole closed month (500000)", composed)
	}
}

func TestEstimateAtDoesNotInflateTodayRevenueFromOtherMonths(t *testing.T) {
	// The bug: revenueOnDay matched by day-of-month number only. An entry from
	// Jul 12 counted as "today's revenue" on Aug 12, inflating todayRevenue
	// and collapsing the projection. This test proves the fix by checking that
	// a huge previous-month entry on the same day number does NOT reduce the
	// official projection.
	//
	// Setup: Aug 2026, today is Aug 12 (Wednesday). We seed enough entries in
	// the trailing window so both paths get identical rates. Then we add a
	// giant entry on Jul 12 — if todayRevenue is inflated by it, the
	// projection drops; if the fix works, the projection is unchanged.

	// Seed a full 8-week window of entries (one per weekday) so rates are
	// well-defined and identical for both entry sets.
	entries := make([]domain.FinancialEntry, 0, 60)
	for d := at12(t, "2026-06-15"); !d.After(at12(t, "2026-08-11")); d = d.AddDate(0, 0, 1) {
		wd := d.Weekday()
		if wd == time.Sunday {
			continue // pharmacy closed Sundays
		}
		entries = append(entries, domain.FinancialEntry{
			TransactionDate: domain.NewCalendarDate(d),
			Amount:          100000, // R$1.000 flat
			Type:            domain.EntryTypeIncome,
			Category:        "venda_balcao",
			Origin:          domain.OriginVenda,
		})
	}

	// Add today's entry (Aug 12) for R$100
	entries = append(entries, sale(t, "2026-08-12", 10000))

	// A huge entry on Jul 12 (same day number) — the old bug would count
	// this as today's revenue, inflating it and collapsing the projection.
	broadEntries := append(slices.Clone(entries), sale(t, "2026-07-12", 5000000))

	monthOnly := estimateAt(entries, "2026-08", at12(t, "2026-08-12"))
	broad := estimateAt(broadEntries, "2026-08", at12(t, "2026-08-12"))

	// The projection must NOT drop when previous-month entries are present.
	// Before the fix, the Jul 12 entry inflated todayRevenue and reduced
	// the official projection significantly.
	if broad.Official < monthOnly.Official {
		t.Errorf("broad entry set lowered projection: month-only=%d, broad=%d (Jul 12 entry leaked into todayRevenue)",
			monthOnly.Official, broad.Official)
	}
}
