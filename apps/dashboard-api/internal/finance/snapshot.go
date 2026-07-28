package finance

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
	"github.com/emerson/emerbot/packages/shared"
)

// SnapshotStore is the slice of the finance store these two endpoints use: the
// four reads analytics.Assemble needs to recompute, plus the snapshot cache
// itself. Six methods instead of the full eighteen-method finance.Store.
type SnapshotStore interface {
	analytics.LedgerReader
	GetInsightSnapshot(ctx context.Context, userID, date string) (finance.InsightSnapshot, error)
	SaveInsightSnapshot(ctx context.Context, userID, date string, snapshot []byte, computedAt time.Time) error
}

// SnapshotHandler serves the cached daily analysis snapshot and supports
// manual recalculation. The snapshot is persisted by the notifier as a
// subproduct of its daily digest run.
type SnapshotHandler struct {
	store SnapshotStore
	loc   *time.Location
	now   func() time.Time
}

// NewSnapshotHandler builds the handler. loc is the timezone whose calendar
// day defines "today" for the snapshot key. The clock is time.Now; tests can
// override it via SetClock.
func NewSnapshotHandler(store SnapshotStore, loc *time.Location) *SnapshotHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &SnapshotHandler{store: store, loc: loc, now: time.Now}
}

// SetClock overrides the time source (tests only).
func (h *SnapshotHandler) SetClock(now func() time.Time) { h.now = now }

// snapshotResponse is the JSON envelope returned by GET /analysis.
type snapshotResponse struct {
	Snapshot   json.RawMessage `json:"snapshot"`
	ComputedAt time.Time       `json:"computedAt"`
	Comparison *comparison     `json:"comparison,omitempty"`
}

// comparison holds the deltas between today's and yesterday's snapshot.
type comparison struct {
	Date   string       `json:"date"`
	Health *healthDelta `json:"health,omitempty"`
	KPIs   *kpiDeltas   `json:"kpis,omitempty"`
}

type healthDelta struct {
	Previous string `json:"previous"`
	Current  string `json:"current"`
	Trend    string `json:"trend"` // "improved" or "downgraded"
}

type kpiDeltas struct {
	Receita   *kpiDelta `json:"receita,omitempty"`
	Despesa   *kpiDelta `json:"despesa,omitempty"`
	Resultado *kpiDelta `json:"resultado,omitempty"`
}

type kpiDelta struct {
	Previous int64   `json:"previous"`
	Current  int64   `json:"current"`
	Delta    int64   `json:"delta"`
	DeltaPct float64 `json:"deltaPct"`
}

// Get handles GET /analysis — returns the cached snapshot for today with an
// optional comparison against yesterday.
func (h *SnapshotHandler) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiauth.ClaimsFromContext(r.Context()); !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := h.now().In(h.loc)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	snap, err := h.store.GetInsightSnapshot(r.Context(), shared.FinanceLedgerID, today)
	if errors.Is(err, finance.ErrInsightNotFound) {
		httpx.Error(w, "snapshot not yet available — run POST /analysis or wait for the daily digest", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("get insight snapshot: %v", err)
		httpx.Error(w, "failed to read snapshot", http.StatusInternalServerError)
		return
	}

	resp := snapshotResponse{
		Snapshot:   snap.Snapshot,
		ComputedAt: snap.ComputedAt,
	}

	// Best-effort comparison with yesterday — if missing, no comparison. A read
	// failure is logged rather than surfaced: today's snapshot is the answer,
	// the delta is garnish, and losing it is not worth failing the request.
	prevSnap, err := h.store.GetInsightSnapshot(r.Context(), shared.FinanceLedgerID, yesterday)
	switch {
	case err == nil:
		resp.Comparison = buildComparison(today, snap.Snapshot, prevSnap.Snapshot)
	case !errors.Is(err, finance.ErrInsightNotFound):
		log.Printf("get yesterday insight snapshot: %v", err)
	}

	httpx.OK(w, resp)
}

// Recalculate handles POST /analysis — recomputes the analysis for the
// current month and persists it as today's snapshot.
func (h *SnapshotHandler) Recalculate(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiauth.ClaimsFromContext(r.Context()); !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := h.now().In(h.loc)
	month := now.Format("2006-01")

	analysis, err := analytics.Assemble(r.Context(), h.store, shared.FinanceLedgerID, month, now)
	if err != nil {
		log.Printf("recalculate analysis error: %v", err)
		httpx.Error(w, "failed to compute analysis", http.StatusInternalServerError)
		return
	}

	snapshotJSON, err := json.Marshal(analysis)
	if err != nil {
		log.Printf("recalculate marshal error: %v", err)
		httpx.Error(w, "failed to serialize analysis", http.StatusInternalServerError)
		return
	}

	today := now.Format("2006-01-02")
	if err := h.store.SaveInsightSnapshot(r.Context(), shared.FinanceLedgerID, today, snapshotJSON, now); err != nil {
		log.Printf("recalculate persist error: %v", err)
		httpx.Error(w, "failed to persist analysis", http.StatusInternalServerError)
		return
	}

	httpx.OK(w, snapshotResponse{
		Snapshot:   snapshotJSON,
		ComputedAt: now,
	})
}

// buildComparison computes deltas between two serialized Analysis snapshots.
// Returns nil if either snapshot cannot be decoded.
func buildComparison(date string, current, previous []byte) *comparison {
	var curr analytics.Analysis
	if err := json.Unmarshal(current, &curr); err != nil {
		return nil
	}
	var prev analytics.Analysis
	if err := json.Unmarshal(previous, &prev); err != nil {
		return nil
	}

	c := &comparison{Date: date}

	// Health status delta. Only emitted when the status actually moved, so
	// there is no "stable" trend to report: the statuses differ, and the only
	// question left is which direction.
	if curr.Health.Status != prev.Health.Status {
		trend := "downgraded"
		if healthRank(curr.Health.Status) < healthRank(prev.Health.Status) {
			trend = "improved"
		}
		c.Health = &healthDelta{
			Previous: string(prev.Health.Status),
			Current:  string(curr.Health.Status),
			Trend:    trend,
		}
	}

	// KPI deltas.
	kpis := &kpiDeltas{}
	if curr.KPIs.Receita != prev.KPIs.Receita {
		kpis.Receita = &kpiDelta{
			Previous: prev.KPIs.Receita,
			Current:  curr.KPIs.Receita,
			Delta:    curr.KPIs.Receita - prev.KPIs.Receita,
			DeltaPct: pctChange(prev.KPIs.Receita, curr.KPIs.Receita),
		}
	}
	if curr.KPIs.Despesa != prev.KPIs.Despesa {
		kpis.Despesa = &kpiDelta{
			Previous: prev.KPIs.Despesa,
			Current:  curr.KPIs.Despesa,
			Delta:    curr.KPIs.Despesa - prev.KPIs.Despesa,
			DeltaPct: pctChange(prev.KPIs.Despesa, curr.KPIs.Despesa),
		}
	}
	if curr.KPIs.Resultado != prev.KPIs.Resultado {
		kpis.Resultado = &kpiDelta{
			Previous: prev.KPIs.Resultado,
			Current:  curr.KPIs.Resultado,
			Delta:    curr.KPIs.Resultado - prev.KPIs.Resultado,
			DeltaPct: pctChange(prev.KPIs.Resultado, curr.KPIs.Resultado),
		}
	}
	if kpis.Receita != nil || kpis.Despesa != nil || kpis.Resultado != nil {
		c.KPIs = kpis
	}

	if c.Health == nil && c.KPIs == nil {
		return nil
	}
	return c
}

// healthRank returns a numeric rank for comparison: higher = worse.
func healthRank(s analytics.HealthStatus) int {
	switch s {
	case analytics.HealthBoa:
		return 0
	case analytics.HealthAtencao:
		return 1
	case analytics.HealthCritico:
		return 2
	}
	return 0 // unknown status, treat as boa
}

// pctChange computes the percentage change from prev to curr.
func pctChange(prev, curr int64) float64 {
	if prev == 0 {
		return 0
	}
	return float64(curr-prev) / float64(prev) * 100
}
