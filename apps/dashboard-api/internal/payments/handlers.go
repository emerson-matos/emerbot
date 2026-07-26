// Package payments serves read-only dashboard views over imported payment-
// processor data (sales, receivables) and the combined cash-flow forecast.
// Writes happen out-of-band via the payment-importer Lambda, so there is no
// import endpoint here.
package payments

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
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
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	from, to, err := monthRange(r)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sales, err := h.repo.ListSales(r.Context(), from, to)
	if err != nil {
		slog.Error("list sales", "error", err)
		httpx.Error(w, "failed to list sales", http.StatusInternalServerError)
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
	httpx.OK(w, map[string]any{
		"sales":     responseSales(sales),
		"totals":    map[string]int64{"gross": gross, "net": net, "fee": fee},
		"by_method": byMethod,
		"from":      from.String(), "to": to.String(),
	})
}

// Receivables handles GET /payments/receivables?from=&to= (defaults to the
// current month), returning the expected receivables plus their total.
func (h *Handler) Receivables(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiauth.ClaimsFromContext(r.Context()); !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	from, to, err := monthRange(r)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	recv, err := h.repo.ListReceivables(r.Context(), from, to)
	if err != nil {
		slog.Error("list receivables", "error", err)
		httpx.Error(w, "failed to list receivables", http.StatusInternalServerError)
		return
	}
	var total int64
	for _, rc := range recv {
		total += rc.Amount
	}
	httpx.OK(w, map[string]any{
		"receivables": responseReceivables(recv), "total": total,
		"from": from.String(), "to": to.String(),
	})
}

// Forecast handles GET /payments/forecast?month=YYYY-MM (defaults to the current
// month): the pharmacy's balance + imported receivables − future expenses.
func (h *Handler) Forecast(w http.ResponseWriter, r *http.Request) {
	claims, ok := apiauth.ClaimsFromContext(r.Context())
	if !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	monthStart, err := time.Parse("2006-01", month)
	if err != nil {
		httpx.Error(w, "invalid month", http.StatusBadRequest)
		return
	}
	monthEnd := monthStart.AddDate(0, 1, -1)

	base, err := h.finStore.CashFlowForecast(r.Context(), claims.UserID, month)
	if err != nil {
		slog.Error("cashflow forecast", "error", err)
		httpx.Error(w, "failed to build forecast", http.StatusInternalServerError)
		return
	}
	recv, err := h.repo.ListReceivables(r.Context(), domain.NewCalendarDate(monthStart), domain.NewCalendarDate(monthEnd))
	if err != nil {
		slog.Error("list receivables", "error", err)
		httpx.Error(w, "failed to build forecast", http.StatusInternalServerError)
		return
	}
	httpx.OK(w, map[string]any{"points": responsePoints(combineForecast(base, recv)), "month": month})
}

// maxRangeDays bounds a from/to window. Each request is a DynamoDB range query
// charged per item read, so an unbounded window ("from=1900-01-01") would read
// the ledger's whole payment history on one unauthenticated-cost path. Two years
// is well beyond any dashboard view while keeping the worst case finite.
const maxRangeDays = 731

// monthRange reads from/to query params (YYYY-MM-DD), defaulting to the current
// calendar month. Malformed or inverted input is an error rather than a silent
// fallback: a typo'd date returning last month's numbers looks like real data.
func monthRange(r *http.Request) (domain.CalendarDate, domain.CalendarDate, error) {
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, -1)

	if f := r.URL.Query().Get("from"); f != "" {
		parsed, err := time.Parse("2006-01-02", f)
		if err != nil {
			return domain.CalendarDate{}, domain.CalendarDate{}, errors.New("invalid from date, expected YYYY-MM-DD")
		}
		from = parsed
	}
	if t := r.URL.Query().Get("to"); t != "" {
		parsed, err := time.Parse("2006-01-02", t)
		if err != nil {
			return domain.CalendarDate{}, domain.CalendarDate{}, errors.New("invalid to date, expected YYYY-MM-DD")
		}
		to = parsed
	}
	if to.Before(from) {
		return domain.CalendarDate{}, domain.CalendarDate{}, errors.New("to must not be before from")
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		return domain.CalendarDate{}, domain.CalendarDate{}, errors.New("date range too large, max 731 days")
	}
	return domain.NewCalendarDate(from), domain.NewCalendarDate(to), nil
}
