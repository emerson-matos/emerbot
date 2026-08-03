package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/payments"
)

// Coverage for the Receivables and Forecast endpoints and the response DTOs,
// alongside the Sales cases already in handlers_test.go.

func recv(t *testing.T, extID, expected string, amount int64, parcela, total int) payments.ExpectedReceivable {
	t.Helper()
	return payments.ExpectedReceivable{
		Provider: payments.ProviderPagBank, SaleID: payments.NewSaleID(payments.ProviderPagBank, extID),
		ExpectedDate: mustDate(t, expected), Amount: amount,
		InstallmentNumber: parcela, InstallmentTotal: total,
	}
}

// failingRepo fails one named list operation so the handlers' 5xx branches run.
type failingRepo struct {
	payments.Repository
	fail string
}

var errRepo = errors.New("repository unavailable")

func (f failingRepo) ListReceivables(ctx context.Context, from, to domain.CalendarDate) ([]payments.ExpectedReceivable, error) {
	if f.fail == "ListReceivables" {
		return nil, errRepo
	}
	return f.Repository.ListReceivables(ctx, from, to)
}

func (f failingRepo) ListSales(ctx context.Context, from, to domain.CalendarDate) ([]payments.Sale, error) {
	if f.fail == "ListSales" {
		return nil, errRepo
	}
	return f.Repository.ListSales(ctx, from, to)
}

// failingFinance fails the ledger forecast the combined view depends on.
type failingFinance struct {
	pkgfinance.Store
}

func (f failingFinance) CashFlowForecast(context.Context, string, string) ([]pkgfinance.CashFlowPoint, error) {
	return nil, errRepo
}

// --- receivables ---

func TestReceivablesReturnsDTOAndTotal(t *testing.T) {
	h := newTestHandler(t, payments.ImportResult{
		Provider: payments.ProviderPagBank, SourceDate: mustDate(t, "2026-07-10"),
		Receivables: []payments.ExpectedReceivable{
			recv(t, "S1", "2026-07-20", 5000, 1, 2),
			recv(t, "S1", "2026-07-25", 4000, 2, 2),
		},
	})

	w := httptest.NewRecorder()
	h.Receivables(w, authed(t, "/payments/receivables?from=2026-07-01&to=2026-07-31"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body)
	}

	var got struct {
		Receivables []struct {
			Provider          string `json:"provider"`
			SaleID            string `json:"sale_id"`
			ExpectedDate      string `json:"expected_date"`
			Amount            int64  `json:"amount"`
			InstallmentNumber int    `json:"installment_number"`
			InstallmentTotal  int    `json:"installment_total"`
		} `json:"receivables"`
		Total int64  `json:"total"`
		From  string `json:"from"`
		To    string `json:"to"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body %s: %v", w.Body, err)
	}
	if len(got.Receivables) != 2 {
		t.Fatalf("receivables = %d, want 2 (body %s)", len(got.Receivables), w.Body)
	}
	if got.Total != 9000 {
		t.Fatalf("total = %d, want 9000", got.Total)
	}
	first := got.Receivables[0]
	if first.Provider != "pagbank" || first.ExpectedDate != "2026-07-20" || first.InstallmentTotal != 2 {
		t.Fatalf("receivable = %+v, want the seeded values in snake_case", first)
	}
}

func TestReceivablesReturnsEmptyArrayNotNull(t *testing.T) {
	w := httptest.NewRecorder()
	newTestHandler(t).Receivables(w, authed(t, "/payments/receivables?from=2026-07-01&to=2026-07-31"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if string(got["receivables"]) != "[]" {
		t.Fatalf("receivables = %s, want []", got["receivables"])
	}
}

func TestReceivablesDefaultsToTheCurrentMonth(t *testing.T) {
	w := httptest.NewRecorder()
	newTestHandler(t).Receivables(w, authed(t, "/payments/receivables"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got struct{ From, To string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	now := time.Now().UTC()
	wantFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	if got.From != wantFrom {
		t.Fatalf("from = %q, want the current month's first day %q", got.From, wantFrom)
	}
}

func TestReceivablesRejectsMalformedDateRange(t *testing.T) {
	cases := []struct{ name, query string }{
		{"unparseable from", "?from=23-07-2026"},
		{"unparseable to", "?to=nope"},
		{"inverted range", "?from=2026-07-31&to=2026-07-01"},
		{"range too large", "?from=1900-01-01&to=2999-12-31"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			newTestHandler(t).Receivables(w, authed(t, "/payments/receivables"+c.query))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body)
			}
		})
	}
}

func TestReceivablesRepositoryFailureIs500(t *testing.T) {
	h := NewHandler(failingRepo{Repository: payments.NewInMemoryRepository(), fail: "ListReceivables"}, pkgfinance.NewInMemoryStore(), time.UTC)
	w := httptest.NewRecorder()
	h.Receivables(w, authed(t, "/payments/receivables"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestSalesRepositoryFailureIs500(t *testing.T) {
	h := NewHandler(failingRepo{Repository: payments.NewInMemoryRepository(), fail: "ListSales"}, pkgfinance.NewInMemoryStore(), time.UTC)
	w := httptest.NewRecorder()
	h.Sales(w, authed(t, "/payments/sales"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

// --- forecast ---

func TestForecastRequiresAuth(t *testing.T) {
	w := httptest.NewRecorder()
	newTestHandler(t).Forecast(w, httptest.NewRequest(http.MethodGet, "/payments/forecast", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestForecastCombinesLedgerAndReceivables(t *testing.T) {
	repo := payments.NewInMemoryRepository()
	if err := repo.Save(context.Background(), payments.ImportResult{
		Provider: payments.ProviderPagBank, SourceDate: mustDate(t, "2026-07-01"),
		Receivables: []payments.ExpectedReceivable{recv(t, "S1", "2026-07-15", 30000, 1, 1)},
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	store := pkgfinance.NewInMemoryStore()
	expense := domain.FinancialEntry{
		UserID: "u1", EntryID: "e1", TransactionDate: mustDate(t, "2026-07-10"),
		Amount: 10000, Category: "aluguel", Type: domain.EntryTypeExpense,
		PaymentStatus: domain.PaymentStatusPending, Source: domain.SourceManual,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.SaveEntry(context.Background(), expense); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	w := httptest.NewRecorder()
	NewHandler(repo, store, time.UTC).Forecast(w, authed(t, "/payments/forecast?month=2026-07"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body)
	}

	var got struct {
		Points []struct {
			Date                string `json:"date"`
			ProjectedReceivable int64  `json:"projected_receivable"`
			ProjectedExpense    int64  `json:"projected_expense"`
			RunningBalance      int64  `json:"running_balance"`
		} `json:"points"`
		Month string `json:"month"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body %s: %v", w.Body, err)
	}
	if got.Month != "2026-07" || len(got.Points) != 31 {
		t.Fatalf("month = %q with %d points, want 2026-07 with 31", got.Month, len(got.Points))
	}

	byDay := map[string]int64{}
	for _, p := range got.Points {
		byDay[p.Date] = p.ProjectedReceivable
	}
	if byDay["2026-07-15"] != 30000 {
		t.Fatalf("receivable on the 15th = %d, want 30000", byDay["2026-07-15"])
	}
	// The month ends with receivables in and the expense out.
	if last := got.Points[30]; last.RunningBalance != 20000 {
		t.Fatalf("final balance = %d, want 30000 received minus 10000 spent", last.RunningBalance)
	}
}

func TestForecastDefaultsToTheCurrentMonth(t *testing.T) {
	w := httptest.NewRecorder()
	newTestHandler(t).Forecast(w, authed(t, "/payments/forecast"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body)
	}

	var got struct{ Month string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Month != time.Now().Format("2006-01") {
		t.Fatalf("month = %q, want the current month", got.Month)
	}
}

func TestForecastLedgerFailureIs500(t *testing.T) {
	h := NewHandler(payments.NewInMemoryRepository(), failingFinance{Store: pkgfinance.NewInMemoryStore()}, time.UTC)
	w := httptest.NewRecorder()
	h.Forecast(w, authed(t, "/payments/forecast?month=2026-07"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestForecastReceivablesFailureIs500(t *testing.T) {
	h := NewHandler(failingRepo{Repository: payments.NewInMemoryRepository(), fail: "ListReceivables"}, pkgfinance.NewInMemoryStore(), time.UTC)
	w := httptest.NewRecorder()
	h.Forecast(w, authed(t, "/payments/forecast?month=2026-07"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestForecastReturnsEmptyArrayNotNull(t *testing.T) {
	// combineForecast returns nil for an empty base; the DTO layer must still
	// serialize [] so the frontend can map over it unguarded.
	w := httptest.NewRecorder()
	newTestHandler(t).Forecast(w, authed(t, "/payments/forecast?month=2026-07"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(got["points"]) == "null" {
		t.Fatal("points serialized as null, want []")
	}
}
