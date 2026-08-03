package payments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/payments"
)

func newTestHandler(t *testing.T, seed ...payments.ImportResult) *Handler {
	t.Helper()
	repo := payments.NewInMemoryRepository()
	for _, r := range seed {
		if err := repo.Save(context.Background(), r); err != nil {
			t.Fatalf("seed repo: %v", err)
		}
	}
	return NewHandler(repo, pkgfinance.NewInMemoryStore(), time.UTC)
}

// authed builds a request carrying verified claims, as the auth middleware
// would have left it.
func authed(t *testing.T, target string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	return r.WithContext(apiauth.WithClaims(r.Context(), apiauth.Claims{UserID: "u1", Subject: "u1"}))
}

func TestSalesRequiresAuth(t *testing.T) {
	w := httptest.NewRecorder()
	// No claims on the context: the handler must not fall through to the store.
	newTestHandler(t).Sales(w, httptest.NewRequest(http.MethodGet, "/payments/sales", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestReceivablesRequiresAuth(t *testing.T) {
	w := httptest.NewRecorder()
	newTestHandler(t).Receivables(w, httptest.NewRequest(http.MethodGet, "/payments/receivables", nil))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// A typo'd date must not quietly return a different period's numbers — on a
// financial dashboard that is indistinguishable from real data.
func TestSalesRejectsMalformedDateRange(t *testing.T) {
	cases := []struct {
		name, query string
	}{
		{"unparseable from", "?from=23-07-2026"},
		{"unparseable to", "?to=nope"},
		{"inverted range", "?from=2026-07-31&to=2026-07-01"},
		{"range too large", "?from=1900-01-01&to=2999-12-31"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			newTestHandler(t).Sales(w, authed(t, "/payments/sales"+c.query))

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d (body %s)", w.Code, http.StatusBadRequest, w.Body)
			}
		})
	}
}

func TestSalesReturnsSnakeCaseDTOAndTotals(t *testing.T) {
	saleID := payments.NewSaleID(payments.ProviderPagBank, "S1")
	day, err := domain.ParseCalendarDate("2026-07-23")
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	h := newTestHandler(t, payments.ImportResult{
		Provider: payments.ProviderPagBank, SourceDate: day,
		Sales: []payments.Sale{{
			ID: saleID, Provider: payments.ProviderPagBank, ExternalID: "S1", SaleDate: day,
			GrossAmount: 10000, NetAmount: 9800, FeeAmount: 200,
			Method: payments.MethodCredito, Brand: "VISA", Installments: 2,
		}},
	})

	w := httptest.NewRecorder()
	h.Sales(w, authed(t, "/payments/sales?from=2026-07-01&to=2026-07-31"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body)
	}
	var got struct {
		Sales []struct {
			ID          string `json:"id"`
			SaleDate    string `json:"sale_date"`
			GrossAmount int64  `json:"gross_amount"`
			FeeAmount   int64  `json:"fee_amount"`
			Method      string `json:"method"`
		} `json:"sales"`
		Totals map[string]int64 `json:"totals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body %s: %v", w.Body, err)
	}
	if len(got.Sales) != 1 {
		t.Fatalf("sales = %d, want 1 (body %s)", len(got.Sales), w.Body)
	}
	s := got.Sales[0]
	if s.ID != string(saleID) || s.SaleDate != "2026-07-23" || s.GrossAmount != 10000 || s.Method != "credito" {
		t.Errorf("sale = %+v, want the seeded sale in snake_case fields", s)
	}
	if got.Totals["gross"] != 10000 || got.Totals["net"] != 9800 || got.Totals["fee"] != 200 {
		t.Errorf("totals = %v, want gross 10000 / net 9800 / fee 200", got.Totals)
	}
}

// An empty range must serialize as [] rather than null, so the frontend needs no
// null guard on a list it is about to map over.
func TestSalesReturnsEmptyArrayNotNull(t *testing.T) {
	w := httptest.NewRecorder()
	newTestHandler(t).Sales(w, authed(t, "/payments/sales?from=2026-07-01&to=2026-07-31"))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if string(got["sales"]) != "[]" {
		t.Errorf("sales = %s, want []", got["sales"])
	}
}

func TestForecastRejectsInvalidMonth(t *testing.T) {
	w := httptest.NewRecorder()
	newTestHandler(t).Forecast(w, authed(t, "/payments/forecast?month=julho"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
