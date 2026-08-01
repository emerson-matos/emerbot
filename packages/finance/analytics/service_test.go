package analytics

import (
	"context"
	"testing"

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
