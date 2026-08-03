package finance

import (
	"context"
	"log"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// SummaryStore is the slice of the finance store the summary endpoints use.
type SummaryStore interface {
	MonthlySummary(ctx context.Context, userID, yearMonth string) (pkgfinance.MonthlySummary, error)
	CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]pkgfinance.CategorySummary, error)
	CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]pkgfinance.CashFlowPoint, error)
}

type SummaryHandler struct {
	store SummaryStore
	// loc is the calendar the pharmacy reasons about days in, for "current
	// month" defaults. See shared.PharmacyLocation.
	loc *time.Location
}

func NewSummaryHandler(store SummaryStore, loc *time.Location) *SummaryHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &SummaryHandler{store: store, loc: loc}
}

// Monthly handles GET /summary/monthly?month=2026-07
func (h *SummaryHandler) Monthly(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	month, err := httpx.Month(r, h.loc)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	summary, err := h.store.MonthlySummary(r.Context(), claims.UserID, month)
	if err != nil {
		log.Printf("monthly summary error: %v", err)
		httpx.Error(w, "failed to get monthly summary", http.StatusInternalServerError)
		return
	}
	httpx.OK(w, summary)
}

// Categories handles GET /summary/categories?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *SummaryHandler) Categories(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	from, to, err := httpx.DateRange(r, h.loc)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cats, err := h.store.CategorySummary(r.Context(), claims.UserID, from, to)
	if err != nil {
		httpx.Error(w, "failed to get category summary", http.StatusInternalServerError)
		return
	}
	httpx.OK(w, map[string]any{
		"categories": cats,
		"from":       domain.NewCalendarDate(from).String(),
		"to":         domain.NewCalendarDate(to).String(),
	})
}

// CashFlow handles GET /summary/cashflow?month=2026-07
func (h *SummaryHandler) CashFlow(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	month, err := httpx.Month(r, h.loc)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	points, err := h.store.CashFlowForecast(r.Context(), claims.UserID, month)
	if err != nil {
		httpx.Error(w, "failed to get cash flow forecast", http.StatusInternalServerError)
		return
	}
	httpx.OK(w, map[string]any{"points": points, "month": month})
}
