package finance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
	"github.com/emerson/emerbot/packages/shared"
)

func snapshotRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	claims := apiauth.Claims{UserID: testUser}
	return req.WithContext(apiauth.WithClaims(req.Context(), claims))
}

func seedSnapshot(t *testing.T, store finance.Store, date string, status analytics.HealthStatus) {
	t.Helper()
	analysis := analytics.Analysis{
		Month: date[:7],
		Health: analytics.Health{
			Status: status,
			Messages: []analytics.Insight{
				{Type: analytics.InsightGoodPerformance, Severity: analytics.SeverityInfo, Title: "ok"},
			},
		},
		KPIs: analytics.KPIs{
			Receita:   100000,
			Despesa:   50000,
			Resultado: 50000,
		},
	}
	snap, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal seed snapshot: %v", err)
	}
	if err := store.SaveInsightSnapshot(context.Background(), shared.FinanceLedgerID, date, snap, time.Now()); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
}

func TestSnapshotGetReturnsCachedAnalysis(t *testing.T) {
	store := finance.NewInMemoryStore()
	today := time.Now().UTC().Format("2006-01-02")
	seedSnapshot(t, store, today, analytics.HealthBoa)

	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp snapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Snapshot) == 0 {
		t.Error("snapshot is empty")
	}
	if resp.ComputedAt.IsZero() {
		t.Error("computedAt is zero")
	}
	if resp.Comparison != nil {
		t.Error("comparison should be nil when no yesterday snapshot")
	}
}

func TestSnapshotGetIncludesComparisonWhenYesterdayExists(t *testing.T) {
	store := finance.NewInMemoryStore()
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	today := time.Now().UTC().Format("2006-01-02")

	seedSnapshot(t, store, yesterday, analytics.HealthBoa)
	seedSnapshot(t, store, today, analytics.HealthAtencao)

	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp snapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Comparison == nil {
		t.Fatal("comparison should not be nil when yesterday snapshot exists")
	}
	if resp.Comparison.Health == nil {
		t.Fatal("health delta should not be nil")
	}
	if resp.Comparison.Health.Previous != "boa" || resp.Comparison.Health.Current != "atencao" {
		t.Errorf("health delta = %+v, want boa→atencao", resp.Comparison.Health)
	}
	if resp.Comparison.Health.Trend != "downgraded" {
		t.Errorf("trend = %q, want downgraded", resp.Comparison.Health.Trend)
	}
}

func TestSnapshotGetReturns404WhenEmpty(t *testing.T) {
	store := finance.NewInMemoryStore()
	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestSnapshotGetRequiresAuth(t *testing.T) {
	store := finance.NewInMemoryStore()
	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Get(rec, httptest.NewRequest(http.MethodGet, "/analysis", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestSnapshotRecalculateComputesAndPersists(t *testing.T) {
	store := finance.NewInMemoryStore()
	// Seed some entries so the analysis has data.
	seedPaidEntry(t, store, time.Now().Format("2006-01")+"-01", 250000, "venda_balcao", domain.EntryTypeIncome)
	seedPaidEntry(t, store, time.Now().Format("2006-01")+"-02", 100000, "aluguel", domain.EntryTypeExpense)

	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Recalculate(rec, snapshotRequest(http.MethodPost, "/analysis"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp snapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Snapshot) == 0 {
		t.Error("snapshot is empty")
	}
	if resp.ComputedAt.IsZero() {
		t.Error("computedAt is zero")
	}

	// Verify it was persisted.
	today := time.Now().UTC().Format("2006-01-02")
	persisted, err := store.GetInsightSnapshot(context.Background(), shared.FinanceLedgerID, today)
	if err != nil {
		t.Fatalf("snapshot not persisted: %v", err)
	}
	if string(persisted.Snapshot) != string(resp.Snapshot) {
		t.Error("persisted snapshot does not match response")
	}
}

func TestSnapshotRecalculateRequiresAuth(t *testing.T) {
	store := finance.NewInMemoryStore()
	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Recalculate(rec, httptest.NewRequest(http.MethodPost, "/analysis", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestComparisonShowsKpiDeltas(t *testing.T) {
	store := finance.NewInMemoryStore()
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	today := time.Now().UTC().Format("2006-01-02")

	// Yesterday: receita 100k
	prev := analytics.Analysis{
		Month:  yesterday[:7],
		Health: analytics.Health{Status: analytics.HealthBoa},
		KPIs:   analytics.KPIs{Receita: 100000, Despesa: 50000, Resultado: 50000},
	}
	prevSnap, _ := json.Marshal(prev)
	if err := store.SaveInsightSnapshot(context.Background(), shared.FinanceLedgerID, yesterday, prevSnap, time.Now()); err != nil {
		t.Fatalf("seed yesterday snapshot: %v", err)
	}

	// Today: receita 120k
	curr := analytics.Analysis{
		Month:  today[:7],
		Health: analytics.Health{Status: analytics.HealthBoa},
		KPIs:   analytics.KPIs{Receita: 120000, Despesa: 45000, Resultado: 75000},
	}
	currSnap, _ := json.Marshal(curr)
	if err := store.SaveInsightSnapshot(context.Background(), shared.FinanceLedgerID, today, currSnap, time.Now()); err != nil {
		t.Fatalf("seed today snapshot: %v", err)
	}

	c := buildComparison(today, currSnap, prevSnap)
	if c == nil {
		t.Fatal("comparison is nil")
	}
	if c.KPIs == nil || c.KPIs.Receita == nil {
		t.Fatal("receita delta missing")
	}
	if c.KPIs.Receita.Delta != 20000 {
		t.Errorf("receita delta = %d, want 20000", c.KPIs.Receita.Delta)
	}
	if c.KPIs.Receita.DeltaPct != 20.0 {
		t.Errorf("receita deltaPct = %.1f, want 20.0", c.KPIs.Receita.DeltaPct)
	}
	if c.KPIs.Despesa == nil {
		t.Fatal("despesa delta missing")
	}
	if c.KPIs.Despesa.Delta != -5000 {
		t.Errorf("despesa delta = %d, want -5000", c.KPIs.Despesa.Delta)
	}
}

func TestComparisonReturnsNilWhenNoChanges(t *testing.T) {
	snap := analytics.Analysis{
		Month:  "2026-07",
		Health: analytics.Health{Status: analytics.HealthBoa},
		KPIs:   analytics.KPIs{Receita: 100000, Despesa: 50000, Resultado: 50000},
	}
	data, _ := json.Marshal(snap)
	c := buildComparison("2026-07-20", data, data)
	if c != nil {
		t.Errorf("comparison should be nil when identical, got %+v", c)
	}
}

func TestSnapshotGetWithYesterdayOnly(t *testing.T) {
	store := finance.NewInMemoryStore()
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	seedSnapshot(t, store, yesterday, analytics.HealthBoa)

	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when only yesterday exists", rec.Code)
	}
}

func TestHealthRankOrdering(t *testing.T) {
	if healthRank(analytics.HealthBoa) >= healthRank(analytics.HealthAtencao) {
		t.Error("boa should rank below atencao")
	}
	if healthRank(analytics.HealthAtencao) >= healthRank(analytics.HealthCritico) {
		t.Error("atencao should rank below critico")
	}
}

func TestPctChange(t *testing.T) {
	if got := pctChange(100, 120); got != 20.0 {
		t.Errorf("pctChange(100,120) = %.1f, want 20.0", got)
	}
	if got := pctChange(100, 80); got != -20.0 {
		t.Errorf("pctChange(100,80) = %.1f, want -20.0", got)
	}
	if got := pctChange(0, 100); got != 0 {
		t.Errorf("pctChange(0,100) = %.1f, want 0", got)
	}
}

// At 22:00 in São Paulo it is already tomorrow in UTC. The snapshot key has to
// follow the pharmacy's calendar, the same one the notifier writes under —
// otherwise the dashboard spends every evening asking for a day that does not
// exist yet and answering 404 with a snapshot sitting right there.
func TestSnapshotGetUsesPharmacyCalendarDayNotUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}
	// 2026-07-21T01:00Z is 2026-07-20 22:00 in São Paulo.
	evening := time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)

	store := finance.NewInMemoryStore()
	seedSnapshot(t, store, "2026-07-20", analytics.HealthBoa)

	handler := NewSnapshotHandler(store, loc)
	handler.SetClock(func() time.Time { return evening })

	rec := httptest.NewRecorder()
	handler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the Brazilian day is 2026-07-20; body = %s", rec.Code, rec.Body.String())
	}

	// And the UTC day must not be what it looks for: a snapshot filed under
	// 2026-07-21 is tomorrow's as far as the pharmacy is concerned.
	utcOnly := finance.NewInMemoryStore()
	seedSnapshot(t, utcOnly, "2026-07-21", analytics.HealthBoa)
	utcHandler := NewSnapshotHandler(utcOnly, loc)
	utcHandler.SetClock(func() time.Time { return evening })

	rec = httptest.NewRecorder()
	utcHandler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 — 2026-07-21 is not today in São Paulo", rec.Code)
	}
}

// The same evening, Recalculate has to file its result under the Brazilian day
// so the Get above finds it.
func TestSnapshotRecalculateUsesPharmacyCalendarDay(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Skipf("zoneinfo unavailable: %v", err)
	}
	evening := time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)

	store := finance.NewInMemoryStore()
	handler := NewSnapshotHandler(store, loc)
	handler.SetClock(func() time.Time { return evening })

	rec := httptest.NewRecorder()
	handler.Recalculate(rec, snapshotRequest(http.MethodPost, "/analysis"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if _, err := store.GetInsightSnapshot(context.Background(), shared.FinanceLedgerID, "2026-07-20"); err != nil {
		t.Errorf("snapshot not filed under the São Paulo day: %v", err)
	}
}

// failingSnapshotStore is an InMemoryStore whose snapshot read is broken — the
// store is up, the item may well exist, the read itself failed.
type failingSnapshotStore struct {
	*finance.InMemoryStore
	err error
}

func (s failingSnapshotStore) GetInsightSnapshot(context.Context, string, string) (finance.InsightSnapshot, error) {
	return finance.InsightSnapshot{}, s.err
}

// A read failure is not an empty cache. Answering 404 here would tell the user
// to wait for a digest that already ran, and hide a broken table behind a
// message that reads like normal operation.
func TestSnapshotGetReturns500OnStoreFailure(t *testing.T) {
	store := failingSnapshotStore{
		InMemoryStore: finance.NewInMemoryStore(),
		err:           errors.New("dynamodb: throughput exceeded"),
	}

	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// ...while a genuinely absent snapshot is still a 404 carrying the sentinel.
func TestSnapshotGet404CarriesSentinel(t *testing.T) {
	store := finance.NewInMemoryStore()
	_, err := store.GetInsightSnapshot(context.Background(), shared.FinanceLedgerID, "2026-07-20")
	if !errors.Is(err, finance.ErrInsightNotFound) {
		t.Fatalf("missing snapshot error = %v, want ErrInsightNotFound", err)
	}

	rec := httptest.NewRecorder()
	handler := NewSnapshotHandler(store, time.UTC)
	handler.Get(rec, snapshotRequest(http.MethodGet, "/analysis"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
