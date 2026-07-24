// Package payments serves read-only dashboard views over imported payment-
// processor data (sales, receivables) and the combined cash-flow forecast.
// Writes happen out-of-band via the payment-importer Lambda, so there is no
// import endpoint here.
package payments

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/payments"
)

// Handler serves the /payments/* read endpoints. It reads canonical data from
// the payments Repository and reuses the finance Store for the forecast's ledger
// balance and future expenses.
type Handler struct {
	repo     payments.Repository
	finStore pkgfinance.Store
}

func NewHandler(repo payments.Repository, finStore pkgfinance.Store) *Handler {
	return &Handler{repo: repo, finStore: finStore}
}

// Sales handles GET /payments/sales?from=YYYY-MM-DD&to=YYYY-MM-DD (defaults to
// the current month), returning the sales plus gross/net/fee totals and a
// per-method breakdown.
func (h *Handler) Sales(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiauth.ClaimsFromContext(r.Context()); !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	from, to := monthRange(r)
	sales, err := h.repo.ListSales(r.Context(), from, to)
	if err != nil {
		log.Printf("list sales error: %v", err)
		jsonError(w, "failed to list sales", http.StatusInternalServerError)
		return
	}

	var gross, net, fee int64
	byMethod := make(map[payments.PaymentMethod]int64)
	for _, s := range sales {
		gross += s.GrossAmount
		net += s.NetAmount
		fee += s.FeeAmount
		byMethod[s.Method] += s.GrossAmount
	}
	jsonOK(w, map[string]any{
		"sales":     sales,
		"totals":    map[string]int64{"gross": gross, "net": net, "fee": fee},
		"by_method": byMethod,
		"from":      from.String(), "to": to.String(),
	})
}

// Receivables handles GET /payments/receivables?from=&to= (defaults to the
// current month), returning the expected receivables plus their total.
func (h *Handler) Receivables(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiauth.ClaimsFromContext(r.Context()); !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	from, to := monthRange(r)
	recv, err := h.repo.ListReceivables(r.Context(), from, to)
	if err != nil {
		log.Printf("list receivables error: %v", err)
		jsonError(w, "failed to list receivables", http.StatusInternalServerError)
		return
	}
	var total int64
	for _, rc := range recv {
		total += rc.Amount
	}
	jsonOK(w, map[string]any{"receivables": recv, "total": total, "from": from.String(), "to": to.String()})
}

// Forecast handles GET /payments/forecast?month=YYYY-MM (defaults to the current
// month): the pharmacy's balance + imported receivables − future expenses.
func (h *Handler) Forecast(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	monthStart, err := time.Parse("2006-01", month)
	if err != nil {
		jsonError(w, "invalid month", http.StatusBadRequest)
		return
	}
	monthEnd := monthStart.AddDate(0, 1, -1)

	base, err := h.finStore.CashFlowForecast(r.Context(), claims.UserID, month)
	if err != nil {
		log.Printf("cashflow forecast error: %v", err)
		jsonError(w, "failed to build forecast", http.StatusInternalServerError)
		return
	}
	recv, err := h.repo.ListReceivables(r.Context(), domain.NewCalendarDate(monthStart), domain.NewCalendarDate(monthEnd))
	if err != nil {
		log.Printf("list receivables error: %v", err)
		jsonError(w, "failed to build forecast", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]any{"points": combineForecast(base, recv), "month": month})
}

// monthRange reads from/to query params (YYYY-MM-DD), defaulting to the current
// calendar month.
func monthRange(r *http.Request) (domain.CalendarDate, domain.CalendarDate) {
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
	return domain.NewCalendarDate(from), domain.NewCalendarDate(to)
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
