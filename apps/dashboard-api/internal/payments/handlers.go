// Package payments serves read-only dashboard views over imported payment-
// processor data (sales, receivables) and the combined cash-flow forecast.
// Writes happen out-of-band via the payment-importer Lambda, so there is no
// import endpoint here.
package payments

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/httpx"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/payments"
)

// LedgerForecaster is the single finance-store method this package needs: the
// ledger side of the combined forecast. It used to depend on the whole
// 17-method Store to call one of them.
type LedgerForecaster interface {
	CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]pkgfinance.CashFlowPoint, error)
}

// Handler serves the /payments/* read endpoints. It reads canonical data from
// the payments Repository and reuses the finance ledger for the forecast's
// balance and future expenses.
type Handler struct {
	repo     payments.Repository
	finStore LedgerForecaster
	// loc is the calendar the pharmacy reasons about days in, for "current
	// month"/"current day" defaults. See shared.PharmacyLocation.
	loc *time.Location
}

func NewHandler(repo payments.Repository, finStore LedgerForecaster, loc *time.Location) *Handler {
	if loc == nil {
		loc = time.UTC
	}
	return &Handler{repo: repo, finStore: finStore, loc: loc}
}

// Sales handles GET /payments/sales?from=YYYY-MM-DD&to=YYYY-MM-DD (defaults to
// the current month), returning the sales plus gross/net/fee totals and a
// per-method breakdown.
func (h *Handler) Sales(w http.ResponseWriter, r *http.Request) {
	if _, ok := apiauth.ClaimsFromContext(r.Context()); !ok {
		httpx.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	from, to, err := httpx.CalendarDateRange(r, h.loc)
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
	from, to, err := httpx.CalendarDateRange(r, h.loc)
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
	month, err := httpx.Month(r, h.loc)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	monthStart, monthEnd, err := domain.ParseMonth(month)
	if err != nil {
		httpx.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

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
