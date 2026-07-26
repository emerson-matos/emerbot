package finance

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
	"github.com/emerson/emerbot/packages/finance/analytics"
)

const testUserID = "shared-ledger"

// analysisRequest builds an authenticated GET against the analysis endpoint.
func analysisRequest(query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/analysis/monthly"+query, nil)
	claims := apiauth.Claims{UserID: testUserID}
	return req.WithContext(apiauth.WithClaims(req.Context(), claims))
}

func seedEntry(t *testing.T, store pkgfinance.Store, date string, amount int64, category string, kind domain.EntryType) {
	t.Helper()
	d, err := domain.ParseCalendarDate(date)
	if err != nil {
		t.Fatalf("parse date %q: %v", date, err)
	}
	entry, err := domain.NewFinancialEntry(domain.NewFinancialEntryInput{
		UserID:          testUserID,
		TransactionDate: d,
		Amount:          amount,
		Category:        category,
		Type:            kind,
		PaymentStatus:   domain.PaymentStatusPaid,
		PaymentDate:     &d,
		Source:          domain.SourceManual,
	})
	if err != nil {
		t.Fatalf("new entry: %v", err)
	}
	if err := store.SaveEntry(context.Background(), entry); err != nil {
		t.Fatalf("save entry: %v", err)
	}
}

func TestAnalysisMonthlyReturnsTheWholeAnalysis(t *testing.T) {
	store := pkgfinance.NewInMemoryStore()
	month := time.Now().Format("2006-01")
	seedEntry(t, store, month+"-01", 250000, "venda_balcao", domain.EntryTypeIncome)
	seedEntry(t, store, month+"-02", 100000, "aluguel", domain.EntryTypeExpense)

	rec := httptest.NewRecorder()
	NewAnalysisHandler(store, time.UTC).Monthly(rec, analysisRequest(""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got analytics.Analysis
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// No month parameter means the current one.
	if got.Month != month {
		t.Errorf("Month = %q, want %q", got.Month, month)
	}
	if got.KPIs.Receita != 250000 || got.KPIs.Despesa != 100000 {
		t.Errorf("KPIs = %+v, want the seeded totals", got.KPIs)
	}
	if len(got.Weekdays) != 7 {
		t.Errorf("Weekdays = %d, want 7", len(got.Weekdays))
	}
	if len(got.ExpenseComposition) != 1 || got.ExpenseComposition[0].CategoryID != "aluguel" {
		t.Errorf("ExpenseComposition = %+v, want the seeded expense", got.ExpenseComposition)
	}
	if len(got.History) != analytics.HistoryMonths {
		t.Errorf("History = %d months, want %d", len(got.History), analytics.HistoryMonths)
	}
}

func TestAnalysisMonthlyRejectsAMalformedMonth(t *testing.T) {
	rec := httptest.NewRecorder()
	NewAnalysisHandler(pkgfinance.NewInMemoryStore(), time.UTC).Monthly(rec, analysisRequest("?month=julho"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a month the caller got wrong", rec.Code)
	}
}

func TestAnalysisMonthlyRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/analysis/monthly", nil)
	NewAnalysisHandler(pkgfinance.NewInMemoryStore(), time.UTC).Monthly(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without claims", rec.Code)
	}
}
