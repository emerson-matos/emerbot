package finance

import (
	"log"
	"net/http"
	"strconv"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

type SummaryHandler struct {
	store pkgfinance.Store
}

func NewSummaryHandler(store pkgfinance.Store) *SummaryHandler {
	return &SummaryHandler{store: store}
}

// Monthly handles GET /summary/monthly?month=2026-07
func (h *SummaryHandler) Monthly(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	summary, err := h.store.MonthlySummary(r.Context(), claims.UserID, month)
	if err != nil {
		log.Printf("monthly summary error: %v", err)
		jsonError(w, "failed to get monthly summary: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, summary)
}

// Categories handles GET /summary/categories?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *SummaryHandler) Categories(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)

	if f := r.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse("2006-01-02", f); err == nil {
			from = t
		}
	}
	if t := r.URL.Query().Get("to"); t != "" {
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			to = parsed
		}
	}

	cats, err := h.store.CategorySummary(r.Context(), claims.UserID, from, to)
	if err != nil {
		jsonError(w, "failed to get category summary", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"categories": cats, "from": from.Format("2006-01-02"), "to": to.Format("2006-01-02")})
}

// CashFlow handles GET /summary/cashflow?days=30
func (h *SummaryHandler) CashFlow(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}

	points, err := h.store.CashFlowForecast(r.Context(), claims.UserID, days)
	if err != nil {
		jsonError(w, "failed to get cash flow forecast", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"points": points, "days": days})
}
