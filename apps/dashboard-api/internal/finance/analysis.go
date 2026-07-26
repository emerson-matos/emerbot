package finance

import (
	"log"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
)

// AnalysisHandler serves the assembled monthly analysis. It replaces the five
// round-trips (entries × 2, summaries, goals, cash flow) the dashboard used to
// make before building the analysis in the browser.
type AnalysisHandler struct {
	store pkgfinance.Store
	loc   *time.Location
}

// NewAnalysisHandler builds the handler. loc is the timezone whose calendar day
// defines "today" for the analysis — nil falls back to UTC, which is a day
// ahead of Brazil for part of every evening.
func NewAnalysisHandler(store pkgfinance.Store, loc *time.Location) *AnalysisHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &AnalysisHandler{store: store, loc: loc}
}

// Monthly handles GET /analysis/monthly?month=2026-07
func (h *AnalysisHandler) Monthly(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := time.Now().In(h.loc)
	month := r.URL.Query().Get("month")
	if month == "" {
		month = now.Format("2006-01")
	}

	analysis, err := analytics.Assemble(r.Context(), h.store, claims.UserID, month, now)
	if err != nil {
		// A malformed month is the caller's mistake, not a server fault, and
		// it is the only failure mode Assemble reports without touching the
		// store.
		if _, _, boundsErr := analytics.MonthBounds(month); boundsErr != nil {
			jsonError(w, "invalid month, use YYYY-MM", http.StatusBadRequest)
			return
		}
		log.Printf("monthly analysis error: %v", err)
		jsonError(w, "failed to build monthly analysis", http.StatusInternalServerError)
		return
	}

	jsonOK(w, analysis)
}
