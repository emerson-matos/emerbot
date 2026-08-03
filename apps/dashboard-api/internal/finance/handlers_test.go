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

// seededDate is the transaction date most of these tests seed their entry on,
// and therefore the date half of its address.
const seededDate = "2026-07-10"

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

// addressed builds a request to /entries/{date}/{id} with both path values
// set, the way the mux fills them in. The date is half the entry's key, so a
// test that omits it is testing a 400, not the handler.
func addressed(method, date, id, body string) *http.Request {
	r := authed(method, "/entries/"+date+"/"+id, body)
	r.SetPathValue("date", date)
	r.SetPathValue("id", id)
	return r
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

func (f failingStore) UpdateEntry(ctx context.Context, previous, updated domain.FinancialEntry) error {
	if f.fail == "UpdateEntry" {
		return errBoom
	}
	return f.Store.UpdateEntry(ctx, previous, updated)
}

func (f failingStore) GetEntry(ctx context.Context, userID string, date domain.CalendarDate, entryID string) (domain.FinancialEntry, error) {
	if f.fail == "GetEntry" {
		return domain.FinancialEntry{}, errBoom
	}
	return f.Store.GetEntry(ctx, userID, date, entryID)
}

func (f failingStore) DeleteEntry(ctx context.Context, userID string, date domain.CalendarDate, entryID string) error {
	if f.fail == "DeleteEntry" {
		return errBoom
	}
	return f.Store.DeleteEntry(ctx, userID, date, entryID)
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
	entries := NewEntriesHandler(store, time.UTC)
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
		"get entry":        {entries.Get, http.MethodGet, "/entries/2026-07-10/e1"},
		"create entry":     {entries.Create, http.MethodPost, "/entries"},
		"update entry":     {entries.Update, http.MethodPut, "/entries/2026-07-10/e1"},
		"delete entry":     {entries.Delete, http.MethodDelete, "/entries/2026-07-10/e1"},
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

	w := run(NewEntriesHandler(store, time.UTC).List, authed(http.MethodGet, "/entries", ""))
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
	w := run(NewEntriesHandler(newStore(t), time.UTC).List, authed(http.MethodGet, "/entries", ""))
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

	w := run(NewEntriesHandler(store, time.UTC).List, authed(http.MethodGet, "/entries?category=aluguel", ""))
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
	h := NewEntriesHandler(store, time.UTC)

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
	w := run(NewEntriesHandler(store, time.UTC).List, authed(http.MethodGet, "/entries?from=2026-07-01&to=2026-07-31", ""))
	assertStatus(t, w, http.StatusOK)
	if got := decode(t, w)["count"]; got != float64(60) {
		t.Fatalf("count = %v, want all 60 entries in the range", got)
	}
}

func TestListEntriesStoreFailureIs500(t *testing.T) {
	h := NewEntriesHandler(failingStore{Store: newStore(t), fail: "ListEntries"}, time.UTC)
	w := run(h.List, authed(http.MethodGet, "/entries", ""))
	assertStatus(t, w, http.StatusInternalServerError)
}

func TestCreateEntry(t *testing.T) {
	store := newStore(t)
	body := `{"date":"2026-07-15","amount":2500,"category":"mercado","type":"expense","description":"Feira","payment_status":"pending","supplier":"Hortifruti"}`

	w := run(NewEntriesHandler(store, time.UTC).Create, authed(http.MethodPost, "/entries", body))
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

	w := run(NewEntriesHandler(store, time.UTC).Create, authed(http.MethodPost, "/entries", body))
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

func TestCreateEntryFutureDatePending(t *testing.T) {
	store := newStore(t)
	// Future date, no payment_status → should be pending with due_date = date.
	body := `{"date":"2099-12-25","amount":5000,"category":"aluguel","type":"expense","description":"Aluguel"}`

	w := run(NewEntriesHandler(store, time.UTC).Create, authed(http.MethodPost, "/entries", body))
	assertStatus(t, w, http.StatusCreated)

	var got entryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PaymentStatus != domain.PaymentStatusPending {
		t.Fatalf("status = %q, want pending for future date", got.PaymentStatus)
	}
	if got.DueDate == nil || *got.DueDate != "2099-12-25" {
		t.Fatalf("due_date = %v, want 2099-12-25", got.DueDate)
	}
	if got.PaymentDate != nil {
		t.Fatalf("payment_date = %v, want nil for pending entry", got.PaymentDate)
	}
}

func TestCreateEntryPastDatePaid(t *testing.T) {
	store := newStore(t)
	// Past date, no payment_status → should be paid with no due_date.
	body := `{"date":"2020-06-01","amount":1000,"category":"mercado","type":"expense","description":"Feira"}`

	w := run(NewEntriesHandler(store, time.UTC).Create, authed(http.MethodPost, "/entries", body))
	assertStatus(t, w, http.StatusCreated)

	var got entryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PaymentStatus != domain.PaymentStatusPaid {
		t.Fatalf("status = %q, want paid for past date", got.PaymentStatus)
	}
	if got.DueDate != nil {
		t.Fatalf("due_date = %v, want nil for past paid entry", got.DueDate)
	}
	if got.PaymentDate == nil {
		t.Fatal("payment_date = nil, want it set for paid entry")
	}
}

func TestCreateEntryRejectsBadInput(t *testing.T) {
	h := NewEntriesHandler(newStore(t), time.UTC)
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
	h := NewEntriesHandler(failingStore{Store: newStore(t), fail: "SaveEntry"}, time.UTC)
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

	r := addressed(http.MethodPut, seededDate, "e1", `{"amount":5000}`)
	w := run(NewEntriesHandler(store, time.UTC).Update, r)
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

// A patch has to distinguish "leave this alone" from "make this empty". These
// are the fields where the two used to be indistinguishable, so a due date or a
// supplier entered by mistake was permanent.
func TestUpdateEntryClearsExplicitlyEmptyFields(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
		d, _ := domain.ParseCalendarDate("2026-07-20")
		e.Description = "original"
		e.Supplier = "fornecedor"
		e.DueDate = &d
	})

	r := addressed(http.MethodPut, seededDate, "e1", `{"description":"","supplier":"","due_date":""}`)
	w := run(NewEntriesHandler(store, time.UTC).Update, r)
	assertStatus(t, w, http.StatusOK)

	var got entryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Description != "" || got.Supplier != "" || got.DueDate != nil {
		t.Fatalf("response = %+v, want description, supplier and due date all cleared", got)
	}
}

// Correcting a mistyped date is the commonest reason to edit a lançamento at
// all, and the field used to be dropped on the floor: the request answered 200
// with the old date still stored.
func TestUpdateEntryMovesTheTransactionDate(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)

	r := addressed(http.MethodPut, seededDate, "e1", `{"date":"2026-08-03"}`)
	w := run(NewEntriesHandler(store, time.UTC).Update, r)
	assertStatus(t, w, http.StatusOK)

	stored, err := store.FindEntryByID(context.Background(), testUser, "e1")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if stored.TransactionDate.String() != "2026-08-03" {
		t.Fatalf("stored transaction date = %q, want 2026-08-03", stored.TransactionDate.String())
	}
}

// An origin is meaningless on money going out, so turning an income entry into
// an expense has to drop it. Validating without normalizing first answered 400
// ("expense entry cannot have an income origin") with nothing the client could
// send to get past it.
func TestUpdateEntryToExpenseDropsTheIncomeOrigin(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
		e.Type = domain.EntryTypeIncome
		e.Origin = domain.OriginVenda
	})

	r := addressed(http.MethodPut, seededDate, "e1", `{"type":"expense"}`)
	w := run(NewEntriesHandler(store, time.UTC).Update, r)
	assertStatus(t, w, http.StatusOK)

	var got entryResponse
	json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
	if got.Type != domain.EntryTypeExpense || got.Origin != "" {
		t.Fatalf("response type/origin = %q/%q, want expense with no origin", got.Type, got.Origin)
	}
}

// An entry written before Origin existed must not acquire one just because
// somebody fixed its description — domain.IsRevenue's shim keeps such an entry
// out of faturamento, and defaulting it to "venda" here would walk a loan into
// the sales figure.
func TestUpdateEntryLeavesAnAbsentOriginAlone(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
		e.Type = domain.EntryTypeIncome
	})

	r := addressed(http.MethodPut, seededDate, "e1", `{"description":"corrigido"}`)
	w := run(NewEntriesHandler(store, time.UTC).Update, r)
	assertStatus(t, w, http.StatusOK)

	var got entryResponse
	json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
	if got.Origin != "" {
		t.Fatalf("origin = %q, want it still unset", got.Origin)
	}
}

func TestUpdateEntryRejectsEmptyRequiredFields(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)
	h := NewEntriesHandler(store, time.UTC)

	// A lançamento always has an amount, a category, a type and a status, so an
	// explicitly empty one is a client bug — answering 200 and changing nothing
	// would report success for an edit that did not happen.
	for name, body := range map[string]string{
		"zero amount":     `{"amount":0}`,
		"negative amount": `{"amount":-100}`,
		"category":        `{"category":"  "}`,
		"type":            `{"type":""}`,
		"payment status":  `{"payment_status":""}`,
		"date":            `{"date":"03/08/2026"}`,
		"due date":        `{"due_date":"agosto"}`,
		"origin":          `{"origin":"pix"}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := addressed(http.MethodPut, seededDate, "e1", body)
			assertStatus(t, run(h.Update, r), http.StatusBadRequest)
		})
	}
}

func TestUpdateEntryPaymentStatusTransitions(t *testing.T) {
	t.Run("pending to paid records today, not the transaction date", func(t *testing.T) {
		store := newStore(t)
		// A bill incurred on the 10th and settled today. Recording the
		// transaction date made the dashboard say "pago em 10/07" no matter
		// when the button was actually pressed.
		seedEntry(t, store, "e1", "2026-07-10", 1000)

		r := addressed(http.MethodPut, seededDate, "e1", `{"payment_status":"paid"}`)
		w := run(NewEntriesHandler(store, time.UTC).Update, r)
		assertStatus(t, w, http.StatusOK)

		var got entryResponse
		json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
		today := time.Now().UTC().Format("2006-01-02")
		if got.PaymentDate == nil || *got.PaymentDate != today {
			t.Fatalf("payment date = %v, want today (%s)", got.PaymentDate, today)
		}
		// The transaction date itself must not move: the bill still belongs to
		// the month it was incurred in.
		if got.TransactionDate != "2026-07-10" {
			t.Fatalf("transaction date = %q, want it untouched at 2026-07-10", got.TransactionDate)
		}
	})

	t.Run("today is the pharmacy's calendar day, not UTC's", func(t *testing.T) {
		store := newStore(t)
		seedEntry(t, store, "e1", "2026-07-10", 1000)

		// A zone far enough east that its calendar day differs from UTC's for
		// part of the day; the recorded date must follow the configured zone.
		loc := time.FixedZone("UTC+14", 14*3600)
		r := addressed(http.MethodPut, seededDate, "e1", `{"payment_status":"paid"}`)
		w := run(NewEntriesHandler(store, loc).Update, r)
		assertStatus(t, w, http.StatusOK)

		var got entryResponse
		json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
		want := time.Now().In(loc).Format("2006-01-02")
		if got.PaymentDate == nil || *got.PaymentDate != want {
			t.Fatalf("payment date = %v, want %s (the configured zone's today)", got.PaymentDate, want)
		}
	})

	t.Run("an already-paid entry keeps its original payment date", func(t *testing.T) {
		store := newStore(t)
		seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
			d, _ := domain.ParseCalendarDate("2026-07-12")
			e.PaymentStatus = domain.PaymentStatusPaid
			e.PaymentDate = &d
		})

		// Re-sending "paid" must not stamp today over the day it was settled.
		r := addressed(http.MethodPut, seededDate, "e1", `{"payment_status":"paid"}`)
		w := run(NewEntriesHandler(store, time.UTC).Update, r)
		assertStatus(t, w, http.StatusOK)

		var got entryResponse
		json.Unmarshal(w.Body.Bytes(), &got) //nolint:errcheck
		if got.PaymentDate == nil || *got.PaymentDate != "2026-07-12" {
			t.Fatalf("payment date = %v, want the original 2026-07-12", got.PaymentDate)
		}
	})

	t.Run("paid to pending clears the payment date", func(t *testing.T) {
		store := newStore(t)
		seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
			d, _ := domain.ParseCalendarDate("2026-07-10")
			e.PaymentStatus = domain.PaymentStatusPaid
			e.PaymentDate = &d
		})

		r := addressed(http.MethodPut, seededDate, "e1", `{"payment_status":"pending"}`)
		w := run(NewEntriesHandler(store, time.UTC).Update, r)
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
	r := addressed(http.MethodPut, seededDate, "e1", `{"type":"transferencia"}`)
	w := run(NewEntriesHandler(store, time.UTC).Update, r)
	assertStatus(t, w, http.StatusBadRequest)

	stored, err := store.FindEntryByID(context.Background(), testUser, "e1")
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
	h := NewEntriesHandler(store, time.UTC)

	t.Run("missing id", func(t *testing.T) {
		w := run(h.Update, authed(http.MethodPut, "/entries/", `{}`))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("unknown entry", func(t *testing.T) {
		r := addressed(http.MethodPut, seededDate, "ghost", `{}`)
		w := run(h.Update, r)
		assertStatus(t, w, http.StatusNotFound)
	})

	t.Run("malformed body", func(t *testing.T) {
		r := addressed(http.MethodPut, seededDate, "e1", `{`)
		w := run(h.Update, r)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("store failure", func(t *testing.T) {
		failing := NewEntriesHandler(failingStore{Store: store, fail: "UpdateEntry"}, time.UTC)
		r := addressed(http.MethodPut, seededDate, "e1", `{"amount":200}`)
		w := run(failing.Update, r)
		assertStatus(t, w, http.StatusInternalServerError)
	})
}

func TestGetEntry(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000, func(e *domain.FinancialEntry) {
		e.Description = "aluguel"
	})
	h := NewEntriesHandler(store, time.UTC)

	t.Run("found", func(t *testing.T) {
		r := addressed(http.MethodGet, seededDate, "e1", "")
		w := run(h.Get, r)
		assertStatus(t, w, http.StatusOK)
		assertJSONContentType(t, w)

		var got entryResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.EntryID != "e1" || got.Description != "aluguel" || got.Amount != 1000 {
			t.Fatalf("response = %+v, want the seeded entry", got)
		}
	})

	t.Run("unknown entry", func(t *testing.T) {
		r := addressed(http.MethodGet, seededDate, "ghost", "")
		assertStatus(t, run(h.Get, r), http.StatusNotFound)
	})

	t.Run("wrong date", func(t *testing.T) {
		// The date is half the key, so the right id under another date
		// addresses a row that is not there.
		r := addressed(http.MethodGet, "2026-01-01", "e1", "")
		assertStatus(t, run(h.Get, r), http.StatusNotFound)
	})

	t.Run("missing id", func(t *testing.T) {
		assertStatus(t, run(h.Get, authed(http.MethodGet, "/entries/", "")), http.StatusBadRequest)
	})

	t.Run("malformed date", func(t *testing.T) {
		r := addressed(http.MethodGet, "10/07/2026", "e1", "")
		assertStatus(t, run(h.Get, r), http.StatusBadRequest)
	})

	/*
	 * A storage failure is not a missing entry. Answering 404 for one told the
	 * user their lançamento did not exist — which on a ledger reads as "your
	 * record is gone", not "try again". It is also what a missing API Gateway
	 * route looks like from the browser, so the two must not be conflated in
	 * the one place that can tell them apart.
	 */
	t.Run("storage failure is not a missing entry", func(t *testing.T) {
		failing := NewEntriesHandler(failingStore{Store: store, fail: "GetEntry"}, time.UTC)
		r := addressed(http.MethodGet, seededDate, "e1", "")
		assertStatus(t, run(failing.Get, r), http.StatusInternalServerError)
	})
}

func TestDeleteEntry(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)
	seedEntry(t, store, "e2", "2026-07-10", 1000)
	h := NewEntriesHandler(store, time.UTC)

	r := addressed(http.MethodDelete, seededDate, "e1", "")
	w := run(h.Delete, r)
	assertStatus(t, w, http.StatusNoContent)

	if _, err := store.FindEntryByID(context.Background(), testUser, "e1"); err == nil {
		t.Fatal("the entry should be gone after a successful delete")
	}

	t.Run("unknown entry is 404", func(t *testing.T) {
		r := addressed(http.MethodDelete, seededDate, "ghost", "")
		assertStatus(t, run(h.Delete, r), http.StatusNotFound)
	})

	t.Run("missing id is 400", func(t *testing.T) {
		assertStatus(t, run(h.Delete, authed(http.MethodDelete, "/entries/", "")), http.StatusBadRequest)
	})

	t.Run("storage failure is 500, not 404", func(t *testing.T) {
		failing := NewEntriesHandler(failingStore{Store: store, fail: "DeleteEntry"}, time.UTC)
		r := addressed(http.MethodDelete, seededDate, "e2", "")
		assertStatus(t, run(failing.Delete, r), http.StatusInternalServerError)
	})
}

// The date in the URL is the entry's address, so an edit that moves it moves
// the address too. The response has to carry the new date or the client has no
// way to find the row it just wrote.
func TestUpdateEntryResponseCarriesTheNewAddress(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)

	r := addressed(http.MethodPut, seededDate, "e1", `{"date":"2026-08-03"}`)
	w := run(NewEntriesHandler(store, time.UTC).Update, r)
	assertStatus(t, w, http.StatusOK)

	var got entryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TransactionDate != "2026-08-03" {
		t.Fatalf("response date = %q, want the new address 2026-08-03", got.TransactionDate)
	}
	// And the old address must no longer resolve.
	old := addressed(http.MethodGet, seededDate, "e1", "")
	assertStatus(t, run(NewEntriesHandler(store, time.UTC).Get, old), http.StatusNotFound)
}

func TestUpdateEntryStorageFailureIsNot404(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)

	failing := NewEntriesHandler(failingStore{Store: store, fail: "GetEntry"}, time.UTC)
	r := addressed(http.MethodPut, seededDate, "e1", `{"amount":200}`)
	assertStatus(t, run(failing.Update, r), http.StatusInternalServerError)
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

// decodeCategories unpacks the handler's {"categories": [...]} envelope.
func decodeCategories(t *testing.T, w *httptest.ResponseRecorder) []domain.Category {
	t.Helper()
	var resp struct {
		Categories []domain.Category `json:"categories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode categories %q: %v", w.Body.String(), err)
	}
	return resp.Categories
}

func findCategory(cats []domain.Category, slug string) *domain.Category {
	for i := range cats {
		if cats[i].Slug == slug {
			return &cats[i]
		}
	}
	return nil
}

func TestListCategoriesBackfillsMissingDefaults(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	// Simulate a user seeded before fornecedor_perfumaria was added to the
	// default list: every default except that one, plus a category the user
	// created themselves.
	for _, c := range domain.DefaultCategories(testUser) {
		if c.Slug == "fornecedor_perfumaria" {
			continue
		}
		if err := store.SaveCategory(ctx, c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	custom := domain.Category{UserID: testUser, Slug: "meu_slug", Label: "Minha Categoria", Type: domain.EntryTypeExpense, Default: false}
	if err := store.SaveCategory(ctx, custom); err != nil {
		t.Fatalf("seed custom: %v", err)
	}

	h := NewCategoriesHandler(store)
	w := run(h.List, authed(http.MethodGet, "/categories", ""))
	assertStatus(t, w, http.StatusOK)

	got := decodeCategories(t, w)
	backfilled := findCategory(got, "fornecedor_perfumaria")
	if backfilled == nil {
		t.Fatal("the missing default category was not returned")
	}
	if !backfilled.Default || backfilled.Label != "Fornecedor de Perfumaria" {
		t.Fatalf("backfilled = %+v, want the current default definition", *backfilled)
	}
	if c := findCategory(got, "meu_slug"); c == nil || c.Label != "Minha Categoria" {
		t.Fatalf("user-created category was lost: %+v", got)
	}

	// The backfill must be persisted, not recomputed.
	stored, err := store.ListCategories(ctx, testUser)
	if err != nil {
		t.Fatalf("list store: %v", err)
	}
	if findCategory(stored, "fornecedor_perfumaria") == nil {
		t.Fatal("the missing default category was not persisted")
	}

	// A second call must be stable: same set, no duplicates, no extra writes.
	w = run(h.List, authed(http.MethodGet, "/categories", ""))
	assertStatus(t, w, http.StatusOK)
	again := decodeCategories(t, w)
	if len(again) != len(got) {
		t.Fatalf("second call returned %d categories, want %d", len(again), len(got))
	}
}

func TestListCategoriesRefreshesDriftedDefaultLabels(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	stale := domain.Category{UserID: testUser, Slug: "aluguel", Label: "Aluguel Antigo", Type: domain.EntryTypeExpense, Default: true}
	if err := store.SaveCategory(ctx, stale); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := NewCategoriesHandler(store)
	w := run(h.List, authed(http.MethodGet, "/categories", ""))
	assertStatus(t, w, http.StatusOK)

	got := decodeCategories(t, w)
	if c := findCategory(got, "aluguel"); c == nil || c.Label != "Aluguel" {
		t.Fatalf("drifted label not refreshed in response: %+v", c)
	}

	stored, err := store.ListCategories(ctx, testUser)
	if err != nil {
		t.Fatalf("list store: %v", err)
	}
	if c := findCategory(stored, "aluguel"); c == nil || c.Label != "Aluguel" {
		t.Fatalf("drifted label not refreshed in store: %+v", c)
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
		t.Fatalf("goal = %+v, want only the income target set", saved)
	}
}

func TestSaveGoalRejectsBadInput(t *testing.T) {
	h := NewGoalsHandler(newStore(t))
	cases := []struct{ name, body, wantErr string }{
		{"malformed json", `{`, "invalid request body"},
		{"no targets", `{"month":"2026-07"}`, "provide at least one of revenue_target or expense_target"},
		{"negative income", `{"month":"2026-07","revenue_target":-1}`, "revenue_target must be >= 0"},
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
	seedEntry(t, store, "in", "2026-07-05", 100000, func(e *domain.FinancialEntry) {
		e.Type = domain.EntryTypeIncome
		// A sale, so faturamento is asserted on an origin rather than on
		// domain.IsRevenue's migration shim.
		e.Origin = domain.OriginVenda
	})
	seedEntry(t, store, "out", "2026-07-10", 30000)

	w := run(NewSummaryHandler(store).Monthly, authed(http.MethodGet, "/summary/monthly?month=2026-07", ""))
	assertStatus(t, w, http.StatusOK)

	body := decode(t, w)
	if body["TotalRevenue"] != float64(100000) || body["TotalExpectedIn"] != float64(100000) || body["TotalExpense"] != float64(30000) || body["ExpectedBalance"] != float64(70000) {
		t.Fatalf("summary = %v, want revenue/expected-in 100000, expense 30000, expected balance 70000", body)
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

func TestCategorySummaryRejectsMalformedDates(t *testing.T) {
	// A malformed from/to used to fall back to the current month, so a typo'd
	// date returned a real period's numbers under the label the user asked
	// for — indistinguishable from correct data on a financial dashboard.
	h := NewSummaryHandler(newStore(t))
	for _, query := range []string{"?from=julho", "?to=agosto", "?from=2026-07-31&to=2026-07-01", "?from=1900-01-01&to=2999-12-31"} {
		t.Run(query, func(t *testing.T) {
			w := run(h.Categories, authed(http.MethodGet, "/summary/categories"+query, ""))
			assertStatus(t, w, http.StatusBadRequest)
		})
	}
}

func TestCategorySummaryDefaultsToTheCurrentMonth(t *testing.T) {
	w := run(NewSummaryHandler(newStore(t)).Categories, authed(http.MethodGet, "/summary/categories", ""))
	assertStatus(t, w, http.StatusOK)

	body := decode(t, w)
	wantFrom, _ := domain.CurrentMonthRange()
	if body["from"] != domain.NewCalendarDate(wantFrom).String() {
		t.Fatalf("from = %v, want the current month's first day", body["from"])
	}
}

func TestMonthParamIsValidated(t *testing.T) {
	store := newStore(t)
	summary := NewSummaryHandler(store)
	goals := NewGoalsHandler(store)

	// Every endpoint taking ?month= must reject a month it cannot parse, so
	// "no data" and "you typed it wrong" never render the same.
	endpoints := map[string]http.HandlerFunc{
		"monthly":  summary.Monthly,
		"cashflow": summary.CashFlow,
		"goals":    goals.Get,
	}
	for name, h := range endpoints {
		for _, month := range []string{"julho", "2026", "2026-13"} {
			t.Run(name+"/"+month, func(t *testing.T) {
				w := run(h, authed(http.MethodGet, "/x?month="+month, ""))
				assertStatus(t, w, http.StatusBadRequest)
			})
		}
	}
}

func TestSaveGoalRejectsAMonthTheOldCheckLetThrough(t *testing.T) {
	store := newStore(t)
	// The previous validation only inspected months starting with "20" that
	// held exactly one dash, so both of these were stored verbatim.
	for _, month := range []string{"julho", "1999-7", "2026-13"} {
		t.Run(month, func(t *testing.T) {
			body := `{"month":"` + month + `","revenue_target":1000}`
			w := run(NewGoalsHandler(store).Save, authed(http.MethodPut, "/goals", body))
			assertStatus(t, w, http.StatusBadRequest)
		})
	}
}

func TestEntriesRejectMalformedDates(t *testing.T) {
	store := newStore(t)
	seedEntry(t, store, "e1", "2026-07-10", 1000)
	h := NewEntriesHandler(store, time.UTC)

	t.Run("list from/to", func(t *testing.T) {
		for _, query := range []string{"?from=julho", "?to=agosto"} {
			w := run(h.List, authed(http.MethodGet, "/entries"+query, ""))
			assertStatus(t, w, http.StatusBadRequest)
		}
	})

	t.Run("create due_date", func(t *testing.T) {
		// A dropped due date loses a bill's deadline, and every due/overdue
		// alert keys off that field — so it must be rejected, not ignored.
		body := `{"amount":100,"category":"mercado","type":"expense","due_date":"20/08/2026"}`
		w := run(h.Create, authed(http.MethodPost, "/entries", body))
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("update due_date", func(t *testing.T) {
		r := addressed(http.MethodPut, seededDate, "e1", `{"due_date":"20/08/2026"}`)
		w := run(h.Update, r)
		assertStatus(t, w, http.StatusBadRequest)

		stored, err := store.FindEntryByID(context.Background(), testUser, "e1")
		if err != nil {
			t.Fatalf("get entry: %v", err)
		}
		if stored.DueDate != nil {
			t.Fatalf("due date = %v, want the rejected update not to have been applied", stored.DueDate)
		}
	})
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
