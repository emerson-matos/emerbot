package analytics

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

func seed(t *testing.T, store pkgfinance.Store, entries ...domain.FinancialEntry) {
	t.Helper()
	for i, e := range entries {
		e.UserID = "u1"
		e.EntryID = domain.EntryID(string(rune('a' + i)))
		e.PaymentStatus = domain.PaymentStatusPaid
		paid := e.TransactionDate
		e.PaymentDate = &paid
		e.Source = domain.SourceManual
		if err := store.SaveEntry(context.Background(), e); err != nil {
			t.Fatalf("save entry %d: %v", i, err)
		}
	}
}

func TestAssemblePullsTheWholeWindow(t *testing.T) {
	ctx := context.Background()
	store := pkgfinance.NewInMemoryStore()

	seed(
		t, store,
		sale(t, "2026-05-10", 100000),
		sale(t, "2026-06-10", 800000),
		sale(t, "2026-07-01", 200000),
		sale(t, "2026-07-14", 150000),
		expense(t, "2026-07-05", "aluguel", 90000),
	)
	if err := store.SaveGoal(ctx, domain.Goal{UserID: "u1", Month: "2026-07", RevenueTarget: 1000000}); err != nil {
		t.Fatalf("save goal: %v", err)
	}

	got, err := Assemble(ctx, store, "u1", "2026-07", at12(t, "2026-07-15"))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	if got.KPIs.Faturamento != 350000 || got.KPIs.Despesa != 90000 {
		t.Errorf("KPIs = %+v, want July's totals", got.KPIs)
	}
	// June's faturamento over the same finished days — through the 14th, since
	// the 15th is still being traded. The whole 800000 falls on the 10th, so
	// all of it counts.
	if got.Trends.Faturamento.Previous != 800000 {
		t.Errorf("Trends.Faturamento.Previous = %d, want 800000", got.Trends.Faturamento.Previous)
	}
	// July's own side stops there too: the 14th sale counts, and only it.
	if got.Trends.Faturamento.Current != 350000 {
		t.Errorf("Trends.Faturamento.Current = %d, want July through the 14th", got.Trends.Faturamento.Current)
	}
	if got.Goals.RevenueTarget != 1000000 {
		t.Errorf("IncomeTarget = %d, want the stored goal", got.Goals.RevenueTarget)
	}
	// May, June and July all carry data, so the window must be fully populated.
	if len(got.History) != HistoryMonths {
		t.Fatalf("History = %d months, want %d", len(got.History), HistoryMonths)
	}
	if got.History[0].Revenue != 100000 || got.History[1].Revenue != 800000 {
		t.Errorf("History = %+v, want May and June filled in", got.History)
	}
	// Only July has a goal; the earlier bars must stay target-less.
	if got.History[0].RevenueTarget != nil {
		t.Errorf("May IncomeTarget = %v, want nil", *got.History[0].RevenueTarget)
	}
	// Down from June's 800000 to July's 350000.
	if got.Trends.Faturamento.Direction != TrendDown {
		t.Errorf("Receita trend = %+v, want down", got.Trends.Faturamento)
	}
}

// The start-of-month case, end to end through the store. On 3 August the shop
// has only traded its opening weekend; the projection has to reach back past the
// month boundary to price the twenty-one weekdays still to come, or it reports a
// quarter of the goal and calls it a forecast.
func TestAssembleProjectsTheStartOfAMonthFromEarlierWeeks(t *testing.T) {
	ctx := context.Background()
	store := pkgfinance.NewInMemoryStore()

	// Eight weeks of trading through July, every day of the week worked.
	var entries []domain.FinancialEntry
	for d := day(t, "2026-06-08").Time(); !d.After(day(t, "2026-07-31").Time()); d = d.AddDate(0, 0, 1) {
		entries = append(entries, sale(t, d.Format("2006-01-02"), 100000))
	}
	// August's own weekend, and nothing else yet.
	entries = append(entries, sale(t, "2026-08-01", 90000), sale(t, "2026-08-02", 60000))
	seed(t, store, entries...)
	if err := store.SaveGoal(ctx, domain.Goal{UserID: "u1", Month: "2026-08", RevenueTarget: 3000000}); err != nil {
		t.Fatalf("save goal: %v", err)
	}

	got, err := Assemble(ctx, store, "u1", "2026-08", at12(t, "2026-08-03"))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// 29 days left at roughly R$1.000,00 a day. Priced from August alone, the
	// 21 weekdays among them counted as zero and Remaining came to a third of
	// this.
	if got.Projection.Remaining < 2500000 {
		t.Errorf("Remaining = %d, want the days left priced from the trailing weeks, not from August's two days",
			got.Projection.Remaining)
	}
	if got.Projection.Basis != ProjectionFromWindow {
		t.Errorf("Basis = %q, want an ordinary projection", got.Projection.Basis)
	}
	// The card now uses the 8-week trailing window, so August's weekdays are
	// priced from July's data. Monday has 8 weeks of data in the window.
	if got.Weekdays[1].Count == 0 {
		t.Errorf("Weekdays[Monday].Count = 0, want window-based data from July")
	}
	if got.Weekdays[1].Basis != ProjectionFromWindow {
		t.Errorf("Weekdays[Monday].Basis = %q, want %q", got.Weekdays[1].Basis, ProjectionFromWindow)
	}
	if got.ToolPayload()["projecao_base"] != string(ProjectionFromWindow) {
		t.Errorf("projecao_base = %v, want the basis spelled out for the bot", got.ToolPayload()["projecao_base"])
	}
}

// countingReader records every range ListEntries was asked for, so a test can
// assert what the assembly actually paid for rather than infer it from output.
type countingReader struct {
	LedgerReader
	ranges []string
}

func (c *countingReader) ListEntries(ctx context.Context, userID string, filter pkgfinance.EntryFilter) ([]domain.FinancialEntry, error) {
	span := "open"
	if filter.From != nil && filter.To != nil {
		span = filter.From.Format("2006-01-02") + ".." + filter.To.Format("2006-01-02")
	}
	c.ranges = append(c.ranges, span)
	return c.LedgerReader.ListEntries(ctx, userID, filter)
}

// A closed month now also fetches the window, because the weekday card uses
// an 8-week Gaussian-weighted average regardless of whether the month is in
// progress. The window is cheap (a single DynamoDB Query on the sort key).
func TestAssembleSkipsTheProjectionWindowForAClosedMonth(t *testing.T) {
	ctx := context.Background()
	store := &countingReader{LedgerReader: pkgfinance.NewInMemoryStore()}

	closed, err := Assemble(ctx, store, "u1", "2026-07", at12(t, "2026-08-03"))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	// July and June on both bases (4 reads) + the 8-week window on both bases
	// (2 reads) = 6 total. The window is always fetched now.
	if len(store.ranges) != 6 {
		t.Errorf("ranges = %v, want the two months plus the window on both bases", store.ranges)
	}
	if closed.Projection.Basis != ProjectionClosed {
		t.Errorf("Basis = %q, want %q — and not %q, which skipping the read would give if the label came off the rates",
			closed.Projection.Basis, ProjectionClosed, ProjectionNoBasis)
	}

	// The month in progress also pays for it: same 6 reads.
	store.ranges = nil
	if _, err := Assemble(ctx, store, "u1", "2026-08", at12(t, "2026-08-03")); err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if len(store.ranges) != 6 {
		t.Errorf("ranges = %v, want the window read on both bases as well", store.ranges)
	}
	var windows int
	for _, r := range store.ranges {
		if r == "2026-06-08..2026-08-02" {
			windows++
		}
	}
	if windows != 2 {
		t.Errorf("ranges = %v, want the eight-week window read twice, once per basis", store.ranges)
	}
}

func TestAssembleRejectsAMalformedMonth(t *testing.T) {
	if _, err := Assemble(context.Background(), pkgfinance.NewInMemoryStore(), "u1", "julho", at12(t, "2026-07-15")); err == nil {
		t.Error("expected an error for a malformed month")
	}
}

func TestAssembleWorksOnAnEmptyLedger(t *testing.T) {
	got, err := Assemble(context.Background(), pkgfinance.NewInMemoryStore(), "u1", "2026-07", at12(t, "2026-07-15"))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if got.Health.Status != HealthBoa || len(got.Recommendations) != 0 {
		t.Errorf("analysis = %+v, want a quiet result rather than an error", got.Health)
	}
}

// The reported case, end to end through the store. On the morning of 3 August
// the panel showed two red recommendations — "Receita caiu 22% abaixo do mês
// passado (até o dia 2)" and "Saldo fica negativo em 1 dia" — to a pharmacy
// that was trading normally and whose balance was never in danger. Both were
// artefacts of the month having just turned over.
func TestThirdOfAugustHasNothingToAlarmAbout(t *testing.T) {
	ctx := context.Background()
	store := pkgfinance.NewInMemoryStore()

	var entries []domain.FinancialEntry
	// July worked every day: R$1.500,00 on weekdays, R$700,00 at weekends.
	for d := day(t, "2026-07-01").Time(); !d.After(day(t, "2026-07-31").Time()); d = d.AddDate(0, 0, 1) {
		amount := int64(150000)
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			amount = 70000
		}
		entries = append(entries, sale(t, d.Format("2006-01-02"), amount))
	}
	// July's outgoings leave R$1.100,00 to open August with.
	entries = append(entries, expense(t, "2026-07-20", "fornecedor_geral", 3900000))
	// August's opening weekend, at exactly July's weekend rate: the pharmacy is
	// doing precisely as well as it was last month.
	entries = append(entries, sale(t, "2026-08-01", 70000), sale(t, "2026-08-02", 70000))
	// And the rent, booked on the 1st for the 5th, as rent is.
	entries = append(entries, expense(t, "2026-08-05", "aluguel", 500000))
	seed(t, store, entries...)

	got, err := Assemble(ctx, store, "u1", "2026-08", at12(t, "2026-08-03"))
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	for _, r := range got.Recommendations {
		if r.Title == "Receita caiu" || r.Title == "Saldo fica negativo em breve" {
			t.Errorf("recommendation %q fired on an ordinary 3rd of the month: %s", r.Title, r.Message)
		}
	}
	if got.CashPosition.DaysUntilNegative != nil {
		t.Errorf("DaysUntilNegative = %d, want none — three ordinary days cover the rent",
			*got.CashPosition.DaysUntilNegative)
	}
	if !got.CashPosition.ExpectsReceipts {
		t.Error("ExpectsReceipts = false, want true — July is right there in the window")
	}

	// The contrast, on the same data: priced from lançamentos alone — the whole
	// month's bills against none of its sales — the balance crosses zero on the
	// 5th, which is the "negativo em 1 dia" the user was shown.
	points, err := store.CashFlowForecast(ctx, "u1", "2026-08")
	if err != nil {
		t.Fatalf("CashFlowForecast: %v", err)
	}
	booked := buildCashPosition(points, dailyRates{}, at12(t, "2026-08-03"))
	if booked.DaysUntilNegative == nil || *booked.DaysUntilNegative != 2 {
		t.Fatalf("booked-only runway = %v, want a crossing in 2 days — the fixture must reproduce the report",
			booked.DaysUntilNegative)
	}

	// And the digest says why there is no comparison rather than going quiet.
	lines := got.DigestLines()
	if last := lines[len(lines)-1]; !strings.Contains(last, "a primeira semana ainda não fechou") {
		t.Errorf("digest ends with %q, want the reason the comparison is missing", last)
	}
}
