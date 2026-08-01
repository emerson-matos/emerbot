package finance

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
	"github.com/emerson/emerbot/packages/shared"
)

// SnapshotHandler serves the cached daily analysis snapshot and supports
// manual recalculation. The snapshot is persisted by the notifier as a
// subproduct of its daily digest run.
type SnapshotHandler struct {
	store finance.Store
	loc   *time.Location
}

// NewSnapshotHandler builds the handler. loc is the timezone whose calendar
// day defines "today" for the snapshot key.
func NewSnapshotHandler(store finance.Store, loc *time.Location) *SnapshotHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &SnapshotHandler{store: store, loc: loc}
}

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
	Trend    string `json:"trend"` // "improved", "downgraded", "stable"
}

type kpiDeltas struct {
	Faturamento *kpiDelta `json:"faturamento,omitempty"`
	Despesa     *kpiDelta `json:"despesa,omitempty"`
	Resultado   *kpiDelta `json:"resultado,omitempty"`
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

	now := time.Now().In(h.loc)
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	snap, err := h.store.GetInsightSnapshot(r.Context(), shared.FinanceLedgerID, today)
	if err != nil {
		httpx.Error(w, "snapshot not found — wait for the daily digest or POST /analysis to compute", http.StatusNotFound)
		return
	}
	// A snapshot from an older schema is served to the frontend raw, so a
	// renamed field would render as R$ 0,00 until the notifier next runs.
	// Treating it as absent sends the client down the path it already handles
	// ("wait for the daily digest"), which is honest about not knowing rather
	// than confidently wrong about the numbers.
	if !snapshotSchemaCurrent(snap.Snapshot) {
		httpx.Error(w, "snapshot is from an older analysis schema — wait for the daily digest or POST /analysis to recompute", http.StatusNotFound)
		return
	}

	resp := snapshotResponse{
		Snapshot:   snap.Snapshot,
		ComputedAt: snap.ComputedAt,
	}

	// Best-effort comparison with yesterday — if missing, no comparison.
	if prevSnap, err := h.store.GetInsightSnapshot(r.Context(), shared.FinanceLedgerID, yesterday); err == nil {
		resp.Comparison = buildComparison(today, snap.Snapshot, prevSnap.Snapshot)
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

	now := time.Now().In(h.loc)
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

// snapshotSchemaCurrent reports whether a stored snapshot was written by the
// current Analysis schema. A snapshot from an older shape decodes without
// error — encoding/json simply leaves renamed fields at their zero value — so
// the version has to be checked explicitly.
func snapshotSchemaCurrent(raw []byte) bool {
	var probe struct {
		Schema int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.Schema == analytics.SchemaVersion
}

// buildComparison computes deltas between two serialized Analysis snapshots.
// Returns nil if either snapshot cannot be decoded.
//
// It also returns nil when the two snapshots were written by different schema
// versions. Comparing across a rename produces confident nonsense rather than
// an error: the old side decodes with the renamed field at zero, so the day a
// field is renamed every user is told their faturamento went from R$ 0,00 to
// the whole month's total. No delta is the correct answer for one day.
func buildComparison(date string, current, previous []byte) *comparison {
	var curr analytics.Analysis
	if err := json.Unmarshal(current, &curr); err != nil {
		return nil
	}
	var prev analytics.Analysis
	if err := json.Unmarshal(previous, &prev); err != nil {
		return nil
	}
	if curr.Schema != prev.Schema {
		return nil
	}

	c := &comparison{Date: date}

	// Health status delta. An "indefinido" on either side is the absence of a
	// verdict, not a better or worse one — the 1st of a month would otherwise
	// report "improved" purely because the month before had closed in the red.
	if curr.Health.Status != prev.Health.Status &&
		curr.Health.Status != analytics.HealthIndefinido &&
		prev.Health.Status != analytics.HealthIndefinido {
		trend := "stable"
		switch {
		case healthRank(curr.Health.Status) > healthRank(prev.Health.Status):
			trend = "downgraded"
		case healthRank(curr.Health.Status) < healthRank(prev.Health.Status):
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
	if curr.KPIs.Faturamento != prev.KPIs.Faturamento {
		kpis.Faturamento = &kpiDelta{
			Previous: prev.KPIs.Faturamento,
			Current:  curr.KPIs.Faturamento,
			Delta:    curr.KPIs.Faturamento - prev.KPIs.Faturamento,
			DeltaPct: pctChange(prev.KPIs.Faturamento, curr.KPIs.Faturamento),
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
	if kpis.Faturamento != nil || kpis.Despesa != nil || kpis.Resultado != nil {
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
	default:
		return 0
	}
}

// pctChange computes the percentage change from prev to curr.
func pctChange(prev, curr int64) float64 {
	if prev == 0 {
		return 0
	}
	return float64(curr-prev) / float64(prev) * 100
}
