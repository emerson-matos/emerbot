package finance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// These tests drive each handler as an http.Handler over the in-memory store,
// so request parsing, status codes and the JSON envelope are all checked
// together — the same path the dashboard actually hits.

const testUser = "shared-ledger"

func newStore(t *testing.T) *pkgfinance.InMemoryStore {
	t.Helper()
	return pkgfinance.NewInMemoryStore()
}

// authed builds a request already carrying trusted claims, standing in for
// what GatewayMiddleware attaches upstream.
func authed(method, target string, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	claims := apiauth.Claims{UserID: testUser, Subject: "cognito-sub", Phone: "+5511987654321"}
	return r.WithContext(apiauth.WithClaims(r.Context(), claims))
}

// anonymous builds a request with no claims at all.
func anonymous(method, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

func run(h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return got
}

func assertStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", w.Code, want, w.Body.String())
	}
}

func assertJSONContentType(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func seedEntry(t *testing.T, store pkgfinance.Store, id, date string, amount int64, mutate ...func(*domain.FinancialEntry)) domain.FinancialEntry {
	t.Helper()
	d, err := domain.ParseCalendarDate(date)
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	e := domain.FinancialEntry{
		UserID: testUser, EntryID: domain.EntryID(id), TransactionDate: d,
		Amount: amount, Category: "mercado", Description: "compra",
		Type: domain.EntryTypeExpense, PaymentStatus: domain.PaymentStatusPending,
		Source: domain.SourceManual, CreatedAt: now, UpdatedAt: now,
	}
	for _, m := range mutate {
		m(&e)
	}
	if err := store.SaveEntry(context.Background(), e); err != nil {
		t.Fatalf("seed entry %s: %v", id, err)
	}
	return e
}

// failingStore wraps a store and fails one named operation, so the handlers'
// 5xx branches can be exercised.
type failingStore struct {
	pkgfinance.Store
	fail string
}

var errBoom = errors.New("storage unavailable")

func (f failingStore) ListEntries(ctx context.Context, userID string, filter pkgfinance.EntryFilter) ([]domain.FinancialEntry, error) {
	if f.fail == "ListEntries" {
		return nil, errBoom
	}
	return f.Store.ListEntries(ctx, userID, filter)
}

func (f failingStore) SaveEntry(ctx context.Context, e domain.FinancialEntry) error {
	if f.fail == "SaveEntry" {
		return errBoom
	}
	return f.Store.SaveEntry(ctx, e)
}

func (f failingStore) UpdateEntry(ctx context.Context, e domain.FinancialEntry) error {
	if f.fail == "UpdateEntry" {
		return errBoom
	}
	return f.Store.UpdateEntry(ctx, e)
}

func (f failingStore) ListCategories(ctx context.Context, userID string) ([]domain.Category, error) {
	if f.fail == "ListCategories" {
		return nil, errBoom
	}
	return f.Store.ListCategories(ctx, userID)
}

func (f failingStore) SaveCategory(ctx context.Context, c domain.Category) error {
	if f.fail == "SaveCategory" {
		return errBoom
	}
	return f.Store.SaveCategory(ctx, c)
}

func (f failingStore) SaveGoal(ctx context.Context, g domain.Goal) error {
	if f.fail == "SaveGoal" {
		return errBoom
	}
	return f.Store.SaveGoal(ctx, g)
}

func (f failingStore) SaveNotificationPrefs(ctx context.Context, p domain.NotificationPrefs) error {
	if f.fail == "SaveNotificationPrefs" {
		return errBoom
	}
	return f.Store.SaveNotificationPrefs(ctx, p)
}

func (f failingStore) MonthlySummary(ctx context.Context, userID, month string) (pkgfinance.MonthlySummary, error) {
	if f.fail == "MonthlySummary" {
		return pkgfinance.MonthlySummary{}, errBoom
	}
	return f.Store.MonthlySummary(ctx, userID, month)
}

func (f failingStore) CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]pkgfinance.CategorySummary, error) {
	if f.fail == "CategorySummary" {
		return nil, errBoom
	}
	return f.Store.CategorySummary(ctx, userID, from, to)
}

func (f failingStore) CashFlowForecast(ctx context.Context, userID, month string) ([]pkgfinance.CashFlowPoint, error) {
	if f.fail == "CashFlowForecast" {
		return nil, errBoom
	}
	return f.Store.CashFlowForecast(ctx, userID, month)
}

// --- shared: every endpoint must reject an unauthenticated request ---

func TestEveryEndpointRequiresClaims(t *testing.T) {
	store := newStore(t)
	entries := NewEntriesHandler(store)
	cats := NewCategoriesHandler(store)
	goals := NewGoalsHandler(store)
	summary := NewSummaryHandler(store)
	notifs := NewNotificationsHandler(store)

	endpoints := map[string]struct {
		h      http.HandlerFunc
		method string
		target string
	}{
		"list entries":     {entries.List, http.MethodGet, "/entries"},
		"create entry":     {entries.Create, http.MethodPost, "/entries"},
		"update entry":     {entries.Update, http.MethodPut, "/entries/e1"},
		"delete entry":     {entries.Delete, http.MethodDelete, "/entries/e1"},
		"list categories":  {cats.List, http.MethodGet, "/categories"},
		"create category":  {cats.Create, http.MethodPost, "/categories"},
		"get goal":         {goals.Get, http.MethodGet, "/goals"},
		"save goal":        {goals.Save, http.MethodPut, "/goals"},
		"monthly summary":  {summary.Monthly, http.MethodGet, "/summary/monthly"},
		"category summary": {summary.Categories, http.MethodGet, "/summary/categories"},
		"cashflow":         {summary.CashFlow, http.MethodGet, "/summary/cashflow"},
		"get prefs":        {notifs.Get, http.MethodGet, "/notifications/preferences"},
		"save prefs":       {notifs.Save, http.MethodPut, "/notifications/preferences"},
	}

	for name, ep := range endpoints {
		t.Run(name, func(t *testing.T) {
			w := run(ep.h, anonymous(ep.method, ep.target))
			assertStatus(t, w, http.StatusUnauthorized)
			if got := decode(t, w)["error"]; got != "unauthorized" {
				t.Fatalf("error = %v, want \"unauthorized\"", got)
			}
		})
	}
}

// --- entries ---

func TestListEntriesReturnsEntriesAndCount(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)
	seedEntry(t, store, "e2", "2026-07-11", 2000)

	w := run(NewEntriesHandler(store).List, authed(http.MethodGet, "/entries", ""))
	assertStatus(t, w, http.StatusOK)
	assertJSONContentType(t, w)

	body := decode(t, w)
	if body["count"] != float64(2) {
		t.Fatalf("count = %v, want 2", body["count"])
	}
	if got := len(body["entries"].([]any)); got != 2 {
		t.Fatalf("entries length = %d, want 2", got)
	}
}

func TestListEntriesIsEmptyNotNullWhenNothingMatches(t *testing.T) {
	w := run(NewEntriesHandler(newStore(t)).List, authed(http.MethodGet, "/entries", ""))
	assertStatus(t, w, http.StatusOK)

	// The dashboard maps over this array, so it must be [] and never null.
	if !strings.Contains(w.Body.String(), `"entries":[]`) {
		t.Fatalf("body = %s, want an empty array for entries", w.Body.String())
	}
}

func TestListEntriesAppliesFilters(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "mercado", "2026-07-10", 1000)
	seedEntry(t, store, "aluguel", "2026-07-11", 2000, func(e *domain.FinancialEntry) {
		e.Category = "aluguel"
	})

	w := run(NewEntriesHandler(store).List, authed(http.MethodGet, "/entries?category=aluguel", ""))
	assertStatus(t, w, http.StatusOK)
	if got := decode(t, w)["count"]; got != float64(1) {
		t.Fatalf("count = %v, want 1 entry matching the category filter", got)
	}
}

func TestListEntriesLimitDefaultsAndCaps(t *testing.T) {
	store := newStore(t)
	for i := range 210 {
		seedEntry(t, store, "e"+string(rune('a'+i/26))+string(rune('a'+i%26)), "2026-07-10", int64(i+1))
	}
	h := NewEntriesHandler(store)

	cases := []struct {
		name   string
		target string
		want   int
	}{
		// Without a date range the endpoint must not scan the whole partition.
		{"defaults when unbounded", "/entries", defaultEntriesLimit},
		{"honours an explicit limit", "/entries?limit=5", 5},
		// A caller asking for more than the cap gets the cap, not the lot.
		{"caps an oversized limit", "/entries?limit=9999", maxEntriesLimit},
		{"ignores a non-numeric limit", "/entries?limit=abc", defaultEntriesLimit},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := run(h.List, authed(http.MethodGet, tc.target, ""))
			assertStatus(t, w, http.StatusOK)
			if got := decode(t, w)["count"]; got != float64(tc.want) {
				t.Fatalf("count = %v, want %d", got, tc.want)
			}
		})
	}
}

func TestListEntriesDateRangeReturnsEveryEntryInThePeriod(t *testing.T) {
	store := newStore(t)
	for i := range 60 {
		seedEntry(t, store, "e"+string(rune('a'+i/26))+string(rune('a'+i%26)), "2026-07-10", int64(i+1))
	}

	// A date-bounded query must not be silently truncated to the default
	// limit, or the dashboard's monthly totals would disagree with its table.
	w := run(NewEntriesHandler(store).List, authed(http.MethodGet, "/entries?from=2026-07-01&to=2026-07-31", ""))
	assertStatus(t, w, http.StatusOK)
	if got := decode(t, w)["count"]; got != float64(60) {
		t.Fatalf("count = %v, want all 60 entries in the range", got)
	}
}

func TestListEntriesStoreFailureIs500(t *testing.T) {
	h := NewEntriesHandler(failingStore{Store: newStore(t), fail: "ListEntries"})
	w := run(h.List, authed(http.MethodGet, "/entries", ""))
	assertStatus(t, w, http.StatusInternalServerError)
}

func TestCreateEntry(t *testing.T) {
	store := newStore(t)
	body := `{"date":"2026-07-15","amount":2500,"category":"mercado","type":"expense","description":"Feira","payment_status":"pending","supplier":"Hortifruti"}`

	w := run(NewEntriesHandler(store).Create, authed(http.MethodPost, "/entries", body))
	assertStatus(t, w, http.StatusCreated)
	assertJSONContentType(t, w)

	var got entryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Amount != 2500 || got.Category != "mercado" || got.TransactionDate != "2026-07-15" {
		t.Fatalf("response = %+v, want the submitted values echoed back", got)
	}
	if got.EntryID == "" {
		t.Fatal("the response must carry the generated entry id")
	}

	stored, err := store.ListEntries(context.Background(), testUser, pkgfinance.EntryFilter{})
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored %d entries (err %v), want 1", len(stored), err)
	}
}

func TestCreateEntryDefaults(t *testing.T) {
	store := newStore(t)
	// No date, no status, no source: today, paid, manual.
	body := `{"amount":100,"category":"mercado","type":"income"}`

	w := run(NewEntriesHandler(store).Create, authed(http.MethodPost, "/entries", body))
	assertStatus(t, w, http.StatusCreated)

	var got entryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TransactionDate != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("date = %q, want today", got.TransactionDate)
	}
	if got.PaymentStatus != domain.PaymentStatusPaid {
		t.Fatalf("status = %q, want it to default to paid", got.PaymentStatus)
	}
	if got.Source != domain.SourceManual {
		t.Fatalf("source = %q, want it to default to manual", got.Source)
	}
}

func TestCreateEntryRejectsBadInput(t *testing.T) {
	h := NewEntriesHandler(newStore(t))
	cases := []struct {
		name, body, wantErr string
	}{
		{"malformed json", `{`, "invalid request body"},
		{"bad date", `{"date":"15/07/2026","amount":100,"category":"c","type":"expense"}`, "invalid date format, use YYYY-MM-DD"},
		{"zero amount", `{"amount":0,"category":"c","type":"expense"}`, "amount must be positive (in centavos)"},
		{"negative amount", `{"amount":-5,"category":"c","type":"expense"}`, "amount must be positive (in centavos)"},
		{"missing category", `{"amount":100,"type":"expense"}`, "category is required"},
		{"bad type", `{"amount":100,"category":"c","type":"transferencia"}`, "type must be 'expense' or 'income'"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := run(h.Create, authed(http.MethodPost, "/entries", tc.body))
			assertStatus(t, w, http.StatusBadRequest)
			if got := decode(t, w)["error"]; got != tc.wantErr {
				t.Fatalf("error = %v, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestCreateEntryStoreFailureIs500(t *testing.T) {
	h := NewEntriesHandler(failingStore{Store: newStore(t), fail: "SaveEntry"})
	body := `{"amount":100,"category":"mercado","type":"expense"}`
	w := run(h.Create, authed(http.MethodPost, "/entries", body))
	assertStatus(t, w, http.StatusInternalServerError)
}

func TestUpdateEntryAppliesOnlySuppliedFields(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
		e.Description = "original"
		e.Supplier = "fornecedor"
	})

	r := authed(http.MethodPut, "/entries/e1", `{"amount":5000}`)
	r.SetPathValue("id", "e1")
	w := run(NewEntriesHandler(store).Update, r)
	assertStatus(t, w, http.StatusOK)

	var got entryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Amount != 5000 {
		t.Fatalf("amount = %d, want the updated 5000", got.Amount)
	}
	// Fields the request omitted must survive untouched.
	if got.Description != "original" || got.Supplier != "fornecedor" {
		t.Fatalf("response = %+v, want the omitted fields preserved", got)
	}
}

func TestUpdateEntryPaymentStatusTransitions(t *testing.T) {
	t.Run("pending to paid sets a payment date", func(t *testing.T) {
		store := newStore(t)
		seedEntry(t, store, "e1", "2026-07-10", 1000)

		r := authed(http.MethodPut, "/entries/e1", `{"payment_status":"paid"}`)
		r.SetPathValue("id", "e1")
		w := run(NewEntriesHandler(store).Update, r)
		assertStatus(t, w, http.StatusOK)

		var got entryResponse
		json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
		if got.PaymentDate == nil || *got.PaymentDate != "2026-07-10" {
			t.Fatalf("payment date = %v, want it filled from the transaction date", got.PaymentDate)
		}
	})

	t.Run("paid to pending clears the payment date", func(t *testing.T) {
		store := newStore(t)
		seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
			d, _ := domain.ParseCalendarDate("2026-07-10")
			e.PaymentStatus = domain.PaymentStatusPaid
			e.PaymentDate = &d
		})

		r := authed(http.MethodPut, "/entries/e1", `{"payment_status":"pending"}`)
		r.SetPathValue("id", "e1")
		w := run(NewEntriesHandler(store).Update, r)
		assertStatus(t, w, http.StatusOK)

		var got entryResponse
		json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
		if got.PaymentDate != nil {
			t.Fatalf("payment date = %v, want it cleared", *got.PaymentDate)
		}
	})
}

func TestUpdateEntryRejectsInvalidBeforeWriting(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)

	// "transferencia" is not a valid entry type. The handler must reject it
	// *and* leave the stored entry alone — validating after the write meant a
	// 400 response and a corrupted ledger.
	r := authed(http.MethodPut, "/entries/e1", `{"type":"transferencia"}`)
	r.SetPathValue("id", "e1")
	w := run(NewEntriesHandler(store).Update, r)
	assertStatus(t, w, http.StatusBadRequest)

	stored, err := store.GetEntry(context.Background(), testUser, "e1")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if stored.Type != domain.EntryTypeExpense {
		t.Fatalf("stored type = %q, want the original expense — the rejected update was persisted", stored.Type)
	}
}

func TestUpdateEntryErrors(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)
	h := NewEntriesHandler(store)

	t.Run("missing id", func(t *testing.T) {
		w := run(h.Update, authed(http.MethodPut, "/entries/", `{}`))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("unknown entry", func(t *testing.T) {
		r := authed(http.MethodPut, "/entries/ghost", `{}`)
		r.SetPathValue("id", "ghost")
		w := run(h.Update, r)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("malformed body", func(t *testing.T) {
		r := authed(http.MethodPut, "/entries/e1", `{`)
		r.SetPathValue("id", "e1")
		w := run(h.Update, r)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("store failure", func(t *testing.T) {
		failing := NewEntriesHandler(failingStore{Store: store, fail: "UpdateEntry"})
		r := authed(http.MethodPut, "/entries/e1", `{"amount":200}`)
		r.SetPathValue("id", "e1")
		w := run(failing.Update, r)
		assertStatus(t, w, http.StatusInternalServerError)
	})
}

func TestDeleteEntry(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)
	h := NewEntriesHandler(store)

	r := authed(http.MethodDelete, "/entries/e1", "")
	r.SetPathValue("id", "e1")
	w := run(h.Delete, r)
	assertStatus(t, w, http.StatusNoContent)

	if _, err := store.GetEntry(context.Background(), testUser, "e1"); err == nil {
		t.Fatal("the entry should be gone after a successful delete")
	}

	t.Run("unknown entry is 404", func(t *testing.T) {
		r := authed(http.MethodDelete, "/entries/ghost", "")
		r.SetPathValue("id", "ghost")
		assertStatus(t, run(h.Delete, r), http.StatusNotFound)
	})

	t.Run("missing id is 400", func(t *testing.T) {
		assertStatus(t, run(h.Delete, authed(http.MethodDelete, "/entries/", "")), http.StatusBadRequest)
	})
}

// --- categories ---

func TestListCategoriesSeedsDefaultsOnFirstCall(t *testing.T) {
	store := newStore(t)
	h := NewCategoriesHandler(store)

	w := run(h.List, authed(http.MethodGet, "/categories", ""))
	assertStatus(t, w, http.StatusOK)
	first := decode(t, w)["categories"].([]any)
	if len(first) == 0 {
		t.Fatal("the first call must seed the default categories")
	}

	// Seeding must be persisted, not recomputed — a second call reads them back
	// from the store and returns the same set.
	w = run(h.List, authed(http.MethodGet, "/categories", ""))
	second := decode(t, w)["categories"].([]any)
	if len(second) != len(first) {
		t.Fatalf("second call returned %d categories, want the %d that were seeded", len(second), len(first))
	}
	stored, err := store.ListCategories(context.Background(), testUser)
	if err != nil || len(stored) != len(first) {
		t.Fatalf("store holds %d categories (err %v), want %d", len(stored), err, len(first))
	}
}

func TestListCategoriesStoreFailureIs500(t *testing.T) {
	h := NewCategoriesHandler(failingStore{Store: newStore(t), fail: "ListCategories"})
	assertStatus(t, run(h.List, authed(http.MethodGet, "/categories", "")), http.StatusInternalServerError)
}

func TestCreateCategory(t *testing.T) {
	store := newStore(t)
	body := `{"slug":"  farmacia  ","label":"  Farmácia  ","type":"expense"}`

	w := run(NewCategoriesHandler(store).Create, authed(http.MethodPost, "/categories", body))
	assertStatus(t, w, http.StatusCreated)

	var got domain.Category
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Slug and label are trimmed, so a stray space cannot create a near-
	// duplicate category.
	if got.Slug != "farmacia" || got.Label != "Farmácia" {
		t.Fatalf("category = %+v, want the slug and label trimmed", got)
	}
	if got.Default {
		t.Fatal("a user-created category must not be marked as a default")
	}
}

func TestCreateCategoryRejectsBadInput(t *testing.T) {
	h := NewCategoriesHandler(newStore(t))
	cases := []struct{ name, body, wantErr string }{
		{"malformed json", `{`, "invalid request body"},
		{"blank slug", `{"slug":"   ","label":"X","type":"expense"}`, "slug and label are required"},
		{"blank label", `{"slug":"x","label":"  ","type":"expense"}`, "slug and label are required"},
		{"bad type", `{"slug":"x","label":"X","type":"outro"}`, "type must be 'expense' or 'income'"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := run(h.Create, authed(http.MethodPost, "/categories", tc.body))
			assertStatus(t, w, http.StatusBadRequest)
			if got := decode(t, w)["error"]; got != tc.wantErr {
				t.Fatalf("error = %v, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestCreateCategoryStoreFailureIs500(t *testing.T) {
	h := NewCategoriesHandler(failingStore{Store: newStore(t), fail: "SaveCategory"})
	body := `{"slug":"x","label":"X","type":"expense"}`
	assertStatus(t, run(h.Create, authed(http.MethodPost, "/categories", body)), http.StatusInternalServerError)
}

// --- goals ---

func TestGetGoalReturnsNullWhenUnset(t *testing.T) {
	w := run(NewGoalsHandler(newStore(t)).Get, authed(http.MethodGet, "/goals?month=2026-07", ""))
	assertStatus(t, w, http.StatusOK)

	body := decode(t, w)
	// A month with no goal is not an error — the form needs something to render.
	if body["goal"] != nil {
		t.Fatalf("goal = %v, want null", body["goal"])
	}
	if body["month"] != "2026-07" {
		t.Fatalf("month = %v, want the requested 2026-07", body["month"])
	}
}

func TestGetGoalDefaultsToCurrentMonth(t *testing.T) {
	w := run(NewGoalsHandler(newStore(t)).Get, authed(http.MethodGet, "/goals", ""))
	assertStatus(t, w, http.StatusOK)
	if got := decode(t, w)["month"]; got != time.Now().Format("2006-01") {
		t.Fatalf("month = %v, want the current month", got)
	}
}

func TestSaveAndGetGoalRoundTrip(t *testing.T) {
	store := newStore(t)
	body := `{"month":"2026-07","revenue_target":500000,"expense_target":300000}`

	w := run(NewGoalsHandler(store).Save, authed(http.MethodPut, "/goals", body))
	assertStatus(t, w, http.StatusOK)

	w = run(NewGoalsHandler(store).Get, authed(http.MethodGet, "/goals?month=2026-07", ""))
	assertStatus(t, w, http.StatusOK)
	goal := decode(t, w)["goal"].(map[string]any)
	if goal["RevenueTarget"] != float64(500000) || goal["ExpenseTarget"] != float64(300000) {
		t.Fatalf("goal = %v, want the saved targets", goal)
	}
}

func TestSaveGoalAcceptsASingleTarget(t *testing.T) {
	store := newStore(t)
	w := run(NewGoalsHandler(store).Save, authed(http.MethodPut, "/goals", `{"month":"2026-07","revenue_target":1000}`))
	assertStatus(t, w, http.StatusOK)

	saved, err := store.GetGoal(context.Background(), testUser, "2026-07")
	if err != nil {
		t.Fatalf("get goal: %v", err)
	}
	if saved.RevenueTarget != 1000 || saved.ExpenseTarget != 0 {
		t.Fatalf("goal = %+v, want only the revenue target set", saved)
	}
}

func TestSaveGoalRejectsBadInput(t *testing.T) {
	h := NewGoalsHandler(newStore(t))
	cases := []struct{ name, body, wantErr string }{
		{"malformed json", `{`, "invalid request body"},
		{"no targets", `{"month":"2026-07"}`, "provide at least one of revenue_target or expense_target"},
		{"negative revenue", `{"month":"2026-07","revenue_target":-1}`, "revenue_target must be >= 0"},
		{"negative expense", `{"month":"2026-07","expense_target":-1}`, "expense_target must be >= 0"},
		{"malformed month", `{"month":"2026-7","revenue_target":1}`, "invalid month format, use YYYY-MM"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := run(h.Save, authed(http.MethodPut, "/goals", tc.body))
			assertStatus(t, w, http.StatusBadRequest)
			if got := decode(t, w)["error"]; got != tc.wantErr {
				t.Fatalf("error = %v, want %q", got, tc.wantErr)
			}
		})
	}
}

func TestSaveGoalStoreFailureIs500(t *testing.T) {
	h := NewGoalsHandler(failingStore{Store: newStore(t), fail: "SaveGoal"})
	body := `{"month":"2026-07","revenue_target":1}`
	assertStatus(t, run(h.Save, authed(http.MethodPut, "/goals", body)), http.StatusInternalServerError)
}

// --- summaries ---

func TestMonthlySummary(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "in", "2026-07-05", 100000, func(e *domain.FinancialEntry) { e.Type = domain.EntryTypeIncome })
	seedEntry(t, store, "out", "2026-07-10", 30000)

	w := run(NewSummaryHandler(store).Monthly, authed(http.MethodGet, "/summary/monthly?month=2026-07", ""))
	assertStatus(t, w, http.StatusOK)

	body := decode(t, w)
	if body["TotalIncome"] != float64(100000) || body["TotalExpense"] != float64(30000) || body["Balance"] != float64(70000) {
		t.Fatalf("summary = %v, want income 100000, expense 30000, balance 70000", body)
	}
}

func TestMonthlySummaryDefaultsToCurrentMonth(t *testing.T) {
	w := run(NewSummaryHandler(newStore(t)).Monthly, authed(http.MethodGet, "/summary/monthly", ""))
	assertStatus(t, w, http.StatusOK)
	if got := decode(t, w)["Month"]; got != time.Now().Format("2006-01") {
		t.Fatalf("month = %v, want the current month", got)
	}
}

func TestMonthlySummaryStoreFailureIs500(t *testing.T) {
	h := NewSummaryHandler(failingStore{Store: newStore(t), fail: "MonthlySummary"})
	assertStatus(t, run(h.Monthly, authed(http.MethodGet, "/summary/monthly", "")), http.StatusInternalServerError)
}

func TestCategorySummary(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-05", 1000)
	seedEntry(t, store, "e2", "2026-07-06", 2000)

	w := run(NewSummaryHandler(store).Categories, authed(http.MethodGet, "/summary/categories?from=2026-07-01&to=2026-07-31", ""))
	assertStatus(t, w, http.StatusOK)

	body := decode(t, w)
	if body["from"] != "2026-07-01" || body["to"] != "2026-07-31" {
		t.Fatalf("range echoed as %v..%v, want the requested dates", body["from"], body["to"])
	}
	cats := body["categories"].([]any)
	if len(cats) != 1 {
		t.Fatalf("got %d categories, want 1", len(cats))
	}
}

func TestCategorySummaryIgnoresMalformedDates(t *testing.T) {
	// A malformed from/to falls back to the current month rather than failing,
	// so the response still describes a real range.
	w := run(NewSummaryHandler(newStore(t)).Categories, authed(http.MethodGet, "/summary/categories?from=julho&to=agosto", ""))
	assertStatus(t, w, http.StatusOK)

	body := decode(t, w)
	now := time.Now().UTC()
	wantFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	if body["from"] != wantFrom {
		t.Fatalf("from = %v, want the current month's first day %v", body["from"], wantFrom)
	}
}

func TestCategorySummaryStoreFailureIs500(t *testing.T) {
	h := NewSummaryHandler(failingStore{Store: newStore(t), fail: "CategorySummary"})
	assertStatus(t, run(h.Categories, authed(http.MethodGet, "/summary/categories", "")), http.StatusInternalServerError)
}

func TestCashFlowSummary(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-05", 1000)

	w := run(NewSummaryHandler(store).CashFlow, authed(http.MethodGet, "/summary/cashflow?month=2026-07", ""))
	assertStatus(t, w, http.StatusOK)

	body := decode(t, w)
	if body["month"] != "2026-07" {
		t.Fatalf("month = %v, want 2026-07", body["month"])
	}
	if got := len(body["points"].([]any)); got != 31 {
		t.Fatalf("got %d points, want one per day in July", got)
	}
}

func TestCashFlowStoreFailureIs500(t *testing.T) {
	h := NewSummaryHandler(failingStore{Store: newStore(t), fail: "CashFlowForecast"})
	assertStatus(t, run(h.CashFlow, authed(http.MethodGet, "/summary/cashflow", "")), http.StatusInternalServerError)
}

// --- notification preferences ---

func TestGetNotificationPrefsFallsBackToDefaults(t *testing.T) {
	w := run(NewNotificationsHandler(newStore(t)).Get, authed(http.MethodGet, "/notifications/preferences", ""))
	assertStatus(t, w, http.StatusOK)

	prefs := decode(t, w)["preferences"].(map[string]any)
	// The phone always comes from the verified Cognito claim, normalized to
	// bare digits for the Meta API.
	if prefs["phone"] != "5511987654321" {
		t.Fatalf("phone = %v, want the claim's digits", prefs["phone"])
	}
}

func TestSaveNotificationPrefsIsPartial(t *testing.T) {
	store := newStore(t)
	h := NewNotificationsHandler(store)

	w := run(h.Save, authed(http.MethodPut, "/notifications/preferences", `{"waEnabled":true,"notifyGoal":true}`))
	assertStatus(t, w, http.StatusOK)

	// A second PUT touching only one field must leave the others as they were.
	w = run(h.Save, authed(http.MethodPut, "/notifications/preferences", `{"notifyGoal":false}`))
	assertStatus(t, w, http.StatusOK)

	prefs := decode(t, w)["preferences"].(map[string]any)
	if prefs["waEnabled"] != true {
		t.Fatalf("waEnabled = %v, want the earlier true to survive a partial update", prefs["waEnabled"])
	}
	if prefs["notifyGoal"] != false {
		t.Fatalf("notifyGoal = %v, want the submitted false", prefs["notifyGoal"])
	}
}

func TestSaveNotificationPrefsIgnoresAClientSuppliedPhone(t *testing.T) {
	store := newStore(t)
	body := `{"waEnabled":true,"phone":"5599999999999"}`

	w := run(NewNotificationsHandler(store).Save, authed(http.MethodPut, "/notifications/preferences", body))
	assertStatus(t, w, http.StatusOK)

	// Honouring a client-supplied number would let anyone redirect another
	// account's WhatsApp alerts to a phone they control.
	saved, err := store.GetNotificationPrefs(context.Background(), "cognito-sub")
	if err != nil {
		t.Fatalf("get prefs: %v", err)
	}
	if saved.Phone != "5511987654321" {
		t.Fatalf("stored phone = %q, want the claim's number, not the request's", saved.Phone)
	}
}

func TestSaveNotificationPrefsRequiresAPhoneToEnableWhatsApp(t *testing.T) {
	store := newStore(t)
	r := httptest.NewRequest(http.MethodPut, "/notifications/preferences", strings.NewReader(`{"waEnabled":true}`))
	// An account with no registered phone number.
	claims := apiauth.Claims{UserID: testUser, Subject: "cognito-sub", Phone: ""}
	r = r.WithContext(apiauth.WithClaims(r.Context(), claims))

	w := run(NewNotificationsHandler(store).Save, r)
	assertStatus(t, w, http.StatusBadRequest)

	if _, err := store.GetNotificationPrefs(context.Background(), "cognito-sub"); err == nil {
		t.Fatal("the rejected preferences must not have been saved")
	}
}

func TestSaveNotificationPrefsRejectsMalformedBody(t *testing.T) {
	h := NewNotificationsHandler(newStore(t))
	w := run(h.Save, authed(http.MethodPut, "/notifications/preferences", `{`))
	assertStatus(t, w, http.StatusBadRequest)
}

func TestSaveNotificationPrefsStoreFailureIs500(t *testing.T) {
	h := NewNotificationsHandler(failingStore{Store: newStore(t), fail: "SaveNotificationPrefs"})
	w := run(h.Save, authed(http.MethodPut, "/notifications/preferences", `{"notifyGoal":true}`))
	assertStatus(t, w, http.StatusInternalServerError)
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"+5511987654321":      "5511987654321",
		"+55 (11) 98765-4321": "5511987654321",
		"":                    "",
		"sem numero":          "",
	}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Fatalf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}
