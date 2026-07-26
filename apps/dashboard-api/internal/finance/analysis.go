package finance

import (
	"log"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/finance/analytics"
)

// AnalysisHandler serves the assembled monthly analysis. It replaces the five
// round-trips (entries × 2, summaries, goals, cash flow) the dashboard used to
// make before building the analysis in the browser.
type AnalysisHandler struct {
	store analytics.LedgerReader
	loc   *time.Location
}

// NewAnalysisHandler builds the handler. loc is the timezone whose calendar day
// defines "today" for the analysis — nil falls back to UTC, which is a day
// ahead of Brazil for part of every evening.
func NewAnalysisHandler(store analytics.LedgerReader, loc *time.Location) *AnalysisHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &AnalysisHandler{store: store, loc: loc}
}

// Monthly handles GET /analysis/monthly?month=2026-07
func (h *AnalysisHandler) Monthly(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := time.Now().In(h.loc)
	// Validated here so a typo comes back as the caller's 400 rather than as a
	// 500 from somewhere inside the assembly.
	month, err := httpx.Month(r)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	analysis, err := analytics.Assemble(r.Context(), h.store, claims.UserID, month, now)
	if err != nil {
		log.Printf("monthly analysis error: %v", err)
		httpx.Error(w, "failed to build monthly analysis", http.StatusInternalServerError)
		return
	}

	httpx.OK(w, analysis)
}
