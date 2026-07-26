package finance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/dynamostore/dynamotest"
)

// These tests drive DynamoDBStore through its real request path against an
// in-memory table (dynamotest), so the key layout, the GSI2 range conditions
// and the filter expressions are all exercised as written — not stubbed out.

const testTable = "emerbot-finance-test"

// newStore returns a store over an empty table shaped like the deployed one:
// PK/SK primary key plus the GSI2-Status index ListEntries queries.
func newStore(t *testing.T, pageSize int) (*DynamoDBStore, *dynamotest.Table) {
	t.Helper()
	tbl := dynamotest.New(dynamotest.Config{
		Name: testTable,
		Key:  dynamotest.Key{Hash: "PK", Range: "SK"},
		GSIs: map[string]dynamotest.Key{
			gsi2IndexName: {Hash: "GSI2PK", Range: "GSI2SK"},
			"GSI1":        {Hash: "GSI1PK", Range: "GSI1SK"},
		},
		PageSize: pageSize,
	})
	return NewDynamoDBStoreWithClient(tbl, testTable), tbl
}

func date(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func dateP(t *testing.T, s string) *domain.CalendarDate {
	t.Helper()
	d := date(t, s)
	return &d
}

func day(t *testing.T, s string) *time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse day %q: %v", s, err)
	}
	return &parsed
}

type entryOpt func(*domain.FinancialEntry)

func withDue(t *testing.T, s string) entryOpt {
	return func(e *domain.FinancialEntry) { e.DueDate = dateP(t, s) }
}

func withCategory(c string) entryOpt {
	return func(e *domain.FinancialEntry) { e.Category = c }
}

func withDescription(d string) entryOpt {
	return func(e *domain.FinancialEntry) { e.Description = d }
}

func withIncome() entryOpt {
	return func(e *domain.FinancialEntry) { e.Type = domain.EntryTypeIncome }
}

func withPaid(t *testing.T, on string) entryOpt {
	return func(e *domain.FinancialEntry) {
		e.PaymentStatus = domain.PaymentStatusPaid
		e.PaymentDate = dateP(t, on)
	}
}

// entry builds a valid pending expense; opts adjust it.
func entry(t *testing.T, id, txDate string, amount int64, opts ...entryOpt) domain.FinancialEntry {
	t.Helper()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	e := domain.FinancialEntry{
		UserID:          "u1",
		EntryID:         domain.EntryID(id),
		TransactionDate: date(t, txDate),
		Amount:          amount,
		Category:        "mercado",
		Description:     "compra",
		Type:            domain.EntryTypeExpense,
		PaymentStatus:   domain.PaymentStatusPending,
		Source:          domain.SourceManual,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

func save(t *testing.T, s *DynamoDBStore, entries ...domain.FinancialEntry) {
	t.Helper()
	for _, e := range entries {
		if err := s.SaveEntry(context.Background(), e); err != nil {
			t.Fatalf("save entry %s: %v", e.EntryID, err)
		}
	}
}

func ids(entries []domain.FinancialEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, string(e.EntryID))
	}
	return out
}

func assertIDs(t *testing.T, got []domain.FinancialEntry, want ...string) {
	t.Helper()
	gotIDs := strings.Join(ids(got), ",")
	if gotIDs != strings.Join(want, ",") {
		t.Fatalf("entry ids = [%s], want [%s]", gotIDs, strings.Join(want, ","))
	}
}

func list(t *testing.T, s *DynamoDBStore, filter EntryFilter) []domain.FinancialEntry {
	t.Helper()
	got, err := s.ListEntries(context.Background(), "u1", filter)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	return got
}

// --- entries ---

func TestSaveAndListEntriesRoundTrip(t *testing.T) {
	s, _ := newStore(t, 0)
	original := entry(t, "e1", "2026-07-10", 12345,
		withCategory("aluguel"), withDescription("Aluguel da Loja"), withPaid(t, "2026-07-11"))
	save(t, s, original)

	got := list(t, s, EntryFilter{})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	// Every field must survive the marshal/unmarshal round trip unchanged.
	if diff := entryDiff(original, got[0]); diff != "" {
		t.Fatalf("entry changed on round trip: %s", diff)
	}
}

func TestSaveEntryRejectsInvalidEntry(t *testing.T) {
	s, tbl := newStore(t, 0)
	bad := entry(t, "e1", "2026-07-10", -5) // negative amount

	if err := s.SaveEntry(context.Background(), bad); err == nil {
		t.Fatal("expected SaveEntry to reject a negative amount")
	}
	if tbl.Len() != 0 {
		t.Fatal("an invalid entry must never reach the table")
	}
}

func TestListEntriesOrdersByEffectiveDateDescending(t *testing.T) {
	s, _ := newStore(t, 0)
	save(
		t, s,
		entry(t, "jan", "2026-01-10", 100),
		entry(t, "dec", "2026-01-10", 100, withDue(t, "2026-12-20")), // registered in Jan, due in Dec
		entry(t, "jul", "2026-07-10", 100),
	)

	assertIDs(t, list(t, s, EntryFilter{}), "dec", "jul", "jan")
}

func TestListEntriesDateRangeUsesEffectiveDate(t *testing.T) {
	s, _ := newStore(t, 0)
	save(
		t, s,
		entry(t, "installment", "2026-07-10", 100, withDue(t, "2026-12-20")),
		entry(t, "julho", "2026-07-15", 100),
	)

	// The December installment was registered in July but must only surface
	// when December is queried — that is the whole point of the GSI2 layout.
	assertIDs(t, list(t, s, EntryFilter{From: day(t, "2026-07-01"), To: day(t, "2026-07-31")}), "julho")
	assertIDs(t, list(t, s, EntryFilter{From: day(t, "2026-12-01"), To: day(t, "2026-12-31")}), "installment")
}

func TestListEntriesRangeIncludesTheLastDay(t *testing.T) {
	s, _ := newStore(t, 0)
	save(t, s, entry(t, "lastday", "2026-07-31", 100))

	// The upper bound is built as "<to>#\xff" precisely so an entry whose
	// GSI2SK carries a trailing id still falls inside an inclusive range.
	assertIDs(t, list(t, s, EntryFilter{From: day(t, "2026-07-01"), To: day(t, "2026-07-31")}), "lastday")
}

func TestListEntriesOpenEndedRanges(t *testing.T) {
	s, _ := newStore(t, 0)
	save(
		t, s,
		entry(t, "old", "2026-01-10", 100),
		entry(t, "new", "2026-09-10", 100),
	)

	assertIDs(t, list(t, s, EntryFilter{From: day(t, "2026-06-01")}), "new")
	assertIDs(t, list(t, s, EntryFilter{To: day(t, "2026-06-01")}), "old")
}

func TestListEntriesFilters(t *testing.T) {
	s, _ := newStore(t, 0)
	save(
		t, s,
		entry(t, "e1", "2026-07-10", 100, withCategory("mercado"), withDescription("Pão na Padaria")),
		entry(t, "e2", "2026-07-11", 200, withCategory("aluguel"), withDescription("Aluguel"), withPaid(t, "2026-07-11")),
		entry(t, "e3", "2026-07-12", 300, withCategory("mercado"), withDescription("feira"), withIncome()),
	)

	cases := []struct {
		name   string
		filter EntryFilter
		want   []string
	}{
		{"by category", EntryFilter{Category: "mercado"}, []string{"e3", "e1"}},
		{"by status", EntryFilter{Status: domain.PaymentStatusPaid}, []string{"e2"}},
		{"by type", EntryFilter{Type: domain.EntryTypeIncome}, []string{"e3"}},
		// Description search must be case-insensitive, which is why the store
		// keeps a lowercased copy of the field to match against.
		{"by description, case-insensitive", EntryFilter{Description: "PADARIA"}, []string{"e1"}},
		{"combined", EntryFilter{Category: "mercado", Type: domain.EntryTypeExpense}, []string{"e1"}},
		{"no match", EntryFilter{Category: "inexistente"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertIDs(t, list(t, s, tc.filter), tc.want...)
		})
	}
}

func TestListEntriesLimitAndCursorPageWithoutLoss(t *testing.T) {
	s, _ := newStore(t, 0)
	// Three entries share one effective date, so a date-only cursor would
	// straddle the page boundary and silently drop one of them.
	save(
		t, s,
		entry(t, "a", "2026-07-10", 100),
		entry(t, "b", "2026-07-10", 100),
		entry(t, "c", "2026-07-10", 100),
		entry(t, "d", "2026-07-09", 100),
	)

	first := list(t, s, EntryFilter{Limit: 2})
	if len(first) != 2 {
		t.Fatalf("first page has %d entries, want 2", len(first))
	}

	last := first[len(first)-1]
	cursor := effectiveDate(last).Format("2006-01-02") + "#" + string(last.EntryID)
	second := list(t, s, EntryFilter{Limit: 2, Cursor: cursor})

	seen := append(ids(first), ids(second)...)
	if len(seen) != 4 {
		t.Fatalf("paging returned %d entries in total, want all 4: %v", len(seen), seen)
	}
	unique := map[string]bool{}
	for _, id := range seen {
		if unique[id] {
			t.Fatalf("entry %q was returned on both pages: %v", id, seen)
		}
		unique[id] = true
	}
}

func TestListEntriesReadsEveryPage(t *testing.T) {
	s, tbl := newStore(t, 2) // force the store's pagination loop to run
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		save(t, s, entry(t, id, "2026-07-10", 100))
	}

	if got := list(t, s, EntryFilter{}); len(got) != 5 {
		t.Fatalf("got %d entries across pages, want 5", len(got))
	}
	if tbl.Calls("Query") < 3 {
		t.Fatalf("expected at least 3 Query calls for 5 items at 2 per page, got %d", tbl.Calls("Query"))
	}
}

func TestListEntriesPropagatesQueryError(t *testing.T) {
	s, tbl := newStore(t, 0)
	boom := errors.New("dynamo is down")
	tbl.FailNext("Query", boom)

	_, err := s.ListEntries(context.Background(), "u1", EntryFilter{})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestListEntriesRejectsCorruptStoredItem(t *testing.T) {
	s, tbl := newStore(t, 0)
	// An item that survives unmarshalling but violates a domain invariant
	// (amount 0) must surface as an error, not as a zero-valued entry.
	err := tbl.Seed(map[string]types.AttributeValue{
		"PK":     &types.AttributeValueMemberS{Value: "USER#u1"},
		"SK":     &types.AttributeValueMemberS{Value: "ENTRY#2026-07-10#bad"},
		"GSI2PK": &types.AttributeValueMemberS{Value: "USER#u1"},
		"GSI2SK": &types.AttributeValueMemberS{Value: "2026-07-10#bad"},
		"UserID": &types.AttributeValueMemberS{Value: "u1"},
		"Date":   &types.AttributeValueMemberS{Value: "2026-07-10"},
		"Amount": &types.AttributeValueMemberN{Value: "0"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := s.ListEntries(context.Background(), "u1", EntryFilter{}); err == nil {
		t.Fatal("expected an error for an entry that fails validation")
	}
}

func TestListEntriesIgnoresOtherUsersAndNonEntryItems(t *testing.T) {
	s, _ := newStore(t, 0)
	ctx := context.Background()

	save(t, s, entry(t, "mine", "2026-07-10", 100))
	other := entry(t, "theirs", "2026-07-10", 100)
	other.UserID = "u2"
	save(t, s, other)

	// Goals and categories live in the same partition but carry no GSI2
	// attributes, so the entries index must never see them.
	if err := s.SaveGoal(ctx, domain.Goal{UserID: "u1", Month: "2026-07", RevenueTarget: 1}); err != nil {
		t.Fatalf("save goal: %v", err)
	}
	if err := s.SaveCategory(ctx, domain.Category{UserID: "u1", Slug: "mercado", Label: "Mercado"}); err != nil {
		t.Fatalf("save category: %v", err)
	}

	assertIDs(t, list(t, s, EntryFilter{}), "mine")
}

func TestSaveEntriesWritesInAtomicChunks(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()

	entries := make([]domain.FinancialEntry, 0, 150)
	for i := range 150 {
		entries = append(entries, entry(t, string(rune('A'+i%26))+string(rune('a'+i/26)), "2026-07-10", 100))
	}

	if err := s.SaveEntries(ctx, entries); err != nil {
		t.Fatalf("save entries: %v", err)
	}
	// 150 entries must be split into two transactions, since DynamoDB caps
	// TransactWriteItems at 100 items.
	if got := tbl.Calls("TransactWriteItems"); got != 2 {
		t.Fatalf("TransactWriteItems calls = %d, want 2", got)
	}
	if got := len(list(t, s, EntryFilter{})); got != 150 {
		t.Fatalf("stored %d entries, want 150", got)
	}
}

func TestSaveEntriesEmptySliceIsNoOp(t *testing.T) {
	s, tbl := newStore(t, 0)
	if err := s.SaveEntries(context.Background(), nil); err != nil {
		t.Fatalf("save no entries: %v", err)
	}
	if tbl.Calls("TransactWriteItems") != 0 {
		t.Fatal("saving nothing must not issue a write")
	}
}

func TestSaveEntriesReportsFailingChunk(t *testing.T) {
	s, tbl := newStore(t, 0)
	entries := make([]domain.FinancialEntry, 0, 150)
	for i := range 150 {
		entries = append(entries, entry(t, string(rune('A'+i%26))+string(rune('a'+i/26)), "2026-07-10", 100))
	}
	// Fail the *second* chunk: the error must name which range failed, since
	// the first chunk stays committed.
	tbl.FailNext("TransactWriteItems", nil)
	tbl.FailNext("TransactWriteItems", errors.New("throughput exceeded"))

	err := s.SaveEntries(context.Background(), entries)
	if err == nil || !strings.Contains(err.Error(), "100-150") {
		t.Fatalf("error = %v, want it to name the failing chunk 100-150", err)
	}
}

func TestGetEntry(t *testing.T) {
	s, _ := newStore(t, 0)
	save(t, s, entry(t, "e1", "2026-07-10", 100), entry(t, "e2", "2026-07-11", 200))

	got, err := s.GetEntry(context.Background(), "u1", "e2")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if got.Amount != 200 {
		t.Fatalf("amount = %d, want 200", got.Amount)
	}

	if _, err := s.GetEntry(context.Background(), "u1", "missing"); err == nil {
		t.Fatal("expected an error for an unknown entry id")
	}
}

func TestUpdateEntryInPlaceWhenDateUnchanged(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()
	save(t, s, entry(t, "e1", "2026-07-10", 100))

	updated := entry(t, "e1", "2026-07-10", 999, withDescription("corrigido"))
	if err := s.UpdateEntry(ctx, updated); err != nil {
		t.Fatalf("update entry: %v", err)
	}

	got := list(t, s, EntryFilter{})
	if len(got) != 1 || got[0].Amount != 999 || got[0].Description != "corrigido" {
		t.Fatalf("got %+v, want a single entry updated to 999/corrigido", got)
	}
	// Same sort key, so a plain put suffices — no transaction needed.
	if tbl.Calls("TransactWriteItems") != 0 {
		t.Fatal("an in-place update must not open a transaction")
	}
}

func TestUpdateEntryMovesItemWhenDateChanges(t *testing.T) {
	s, tbl := newStore(t, 0)
	ctx := context.Background()
	save(t, s, entry(t, "e1", "2026-07-10", 100))

	moved := entry(t, "e1", "2026-08-20", 100)
	if err := s.UpdateEntry(ctx, moved); err != nil {
		t.Fatalf("update entry: %v", err)
	}

	got := list(t, s, EntryFilter{})
	if len(got) != 1 {
		// The old sort key must be deleted in the same transaction as the new
		// put, or the entry would exist twice under two dates.
		t.Fatalf("got %d entries after moving the date, want 1: %v", len(got), ids(got))
	}
	if got[0].TransactionDate.String() != "2026-08-20" {
		t.Fatalf("date = %s, want 2026-08-20", got[0].TransactionDate)
	}
	if tbl.Calls("TransactWriteItems") != 1 {
		t.Fatalf("expected the move to run as one transaction, got %d", tbl.Calls("TransactWriteItems"))
	}
}

func TestUpdateEntryRejectsUnknownAndInvalid(t *testing.T) {
	s, _ := newStore(t, 0)
	ctx := context.Background()

	if err := s.UpdateEntry(ctx, entry(t, "ghost", "2026-07-10", 100)); err == nil {
		t.Fatal("expected an error updating an entry that does not exist")
	}

	save(t, s, entry(t, "e1", "2026-07-10", 100))
	if err := s.UpdateEntry(ctx, entry(t, "e1", "2026-07-10", -1)); err == nil {
		t.Fatal("expected an error updating with an invalid amount")
	}
}

func TestDeleteEntry(t *testing.T) {
	s, _ := newStore(t, 0)
	ctx := context.Background()
	save(t, s, entry(t, "e1", "2026-07-10", 100), entry(t, "e2", "2026-07-11", 200))

	if err := s.DeleteEntry(ctx, "u1", "e1"); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	assertIDs(t, list(t, s, EntryFilter{}), "e2")

	if err := s.DeleteEntry(ctx, "u1", "missing"); err == nil {
		t.Fatal("expected an error deleting an unknown entry")
	}
}

// --- summaries ---

func TestMonthlySummary(t *testing.T) {
	s, _ := newStore(t, 0)
	save(
		t, s,
		entry(t, "in", "2026-07-05", 100000, withIncome()),
		entry(t, "out1", "2026-07-10", 30000),
		entry(t, "out2", "2026-07-31", 20000),
		entry(t, "other-month", "2026-08-01", 999999),
	)

	got, err := s.MonthlySummary(context.Background(), "u1", "2026-07")
	if err != nil {
		t.Fatalf("monthly summary: %v", err)
	}
	want := MonthlySummary{Month: "2026-07", TotalIncome: 100000, TotalExpense: 50000, Balance: 50000}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
}

func TestMonthlySummaryRejectsBadMonth(t *testing.T) {
	s, _ := newStore(t, 0)
	if _, err := s.MonthlySummary(context.Background(), "u1", "julho"); err == nil {
		t.Fatal("expected an error for a malformed yearMonth")
	}
}

func TestCategorySummarySortsByTotalDescending(t *testing.T) {
	s, _ := newStore(t, 0)
	save(
		t, s,
		entry(t, "a", "2026-07-05", 1000, withCategory("mercado")),
		entry(t, "b", "2026-07-06", 2000, withCategory("mercado")),
		entry(t, "c", "2026-07-07", 5000, withCategory("aluguel")),
	)

	got, err := s.CategorySummary(context.Background(), "u1", *day(t, "2026-07-01"), *day(t, "2026-07-31"))
	if err != nil {
		t.Fatalf("category summary: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d categories, want 2", len(got))
	}
	if got[0].Category != "aluguel" || got[0].Total != 5000 || got[0].Count != 1 {
		t.Fatalf("first category = %+v, want aluguel/5000/1", got[0])
	}
	if got[1].Category != "mercado" || got[1].Total != 3000 || got[1].Count != 2 {
		t.Fatalf("second category = %+v, want mercado/3000/2", got[1])
	}
}

func TestCashFlowForecast(t *testing.T) {
	s, _ := newStore(t, 0)
	save(
		t, s,
		entry(t, "past-in", "2026-06-15", 50000, withIncome()), // opening balance
		entry(t, "past-out", "2026-06-20", 20000),
		entry(t, "jul-out", "2026-07-02", 5000),
		entry(t, "jul-in", "2026-07-03", 10000, withIncome()),
	)

	points, err := s.CashFlowForecast(context.Background(), "u1", "2026-07")
	if err != nil {
		t.Fatalf("cash flow forecast: %v", err)
	}
	if len(points) != 31 {
		t.Fatalf("got %d points, want 31 days in July", len(points))
	}

	// Day 1 carries the opening balance only; June's entries must not be
	// double counted into July.
	if points[0].RunningBalance != 30000 {
		t.Fatalf("day 1 balance = %d, want the 30000 carried in from June", points[0].RunningBalance)
	}
	if points[1].ProjectedExpense != 5000 || points[1].RunningBalance != 25000 {
		t.Fatalf("day 2 = %+v, want expense 5000 and balance 25000", points[1])
	}
	if points[2].ProjectedIncome != 10000 || points[2].RunningBalance != 35000 {
		t.Fatalf("day 3 = %+v, want income 10000 and balance 35000", points[2])
	}
	if last := points[30]; last.Date != "2026-07-31" || last.RunningBalance != 35000 {
		t.Fatalf("last point = %+v, want 2026-07-31 holding the balance flat", last)
	}
}

func TestCashFlowForecastRejectsBadMonth(t *testing.T) {
	s, _ := newStore(t, 0)
	if _, err := s.CashFlowForecast(context.Background(), "u1", "nope"); err == nil {
		t.Fatal("expected an error for a malformed month")
	}
}

// --- goals and categories ---

func TestGoalRoundTrip(t *testing.T) {
	s, _ := newStore(t, 0)
	ctx := context.Background()
	goal := domain.Goal{UserID: "u1", Month: "2026-07", RevenueTarget: 500000, ExpenseTarget: 300000}

	if err := s.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("save goal: %v", err)
	}
	got, err := s.GetGoal(ctx, "u1", "2026-07")
	if err != nil {
		t.Fatalf("get goal: %v", err)
	}
	if got != goal {
		t.Fatalf("goal = %+v, want %+v", got, goal)
	}

	if _, err := s.GetGoal(ctx, "u1", "2026-08"); err == nil {
		t.Fatal("expected an error for a month with no goal")
	}
}

func TestGoalGetPropagatesError(t *testing.T) {
	s, tbl := newStore(t, 0)
	boom := errors.New("read failed")
	tbl.FailNext("GetItem", boom)

	if _, err := s.GetGoal(context.Background(), "u1", "2026-07"); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestListCategoriesSortedAndScopedToUser(t *testing.T) {
	s, _ := newStore(t, 0)
	ctx := context.Background()
	for _, c := range []domain.Category{
		{UserID: "u1", Slug: "mercado", Label: "Mercado", Type: domain.EntryTypeExpense},
		{UserID: "u1", Slug: "aluguel", Label: "Aluguel", Type: domain.EntryTypeExpense, Default: true},
		{UserID: "u2", Slug: "outro", Label: "Outro"},
	} {
		if err := s.SaveCategory(ctx, c); err != nil {
			t.Fatalf("save category %s: %v", c.Slug, err)
		}
	}
	// An entry shares the partition; the CAT# prefix must exclude it.
	save(t, s, entry(t, "e1", "2026-07-10", 100))

	got, err := s.ListCategories(ctx, "u1")
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(got) != 2 || got[0].Slug != "aluguel" || got[1].Slug != "mercado" {
		t.Fatalf("categories = %+v, want aluguel then mercado", got)
	}
	if !got[0].Default || got[0].Label != "Aluguel" {
		t.Fatalf("first category lost its fields: %+v", got[0])
	}
}

func TestListCategoriesPropagatesError(t *testing.T) {
	s, tbl := newStore(t, 0)
	boom := errors.New("query failed")
	tbl.FailNext("Query", boom)

	if _, err := s.ListCategories(context.Background(), "u1"); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

// --- notifications ---

func TestNotificationPrefsRoundTrip(t *testing.T) {
	s, _ := newStore(t, 0)
	ctx := context.Background()
	prefs := domain.NotificationPrefs{
		UserID: "u1", WAEnabled: true, Phone: "+5511999999999",
		NotifyDueToday: true, NotifyOverdue: false, NotifyGoal: true,
	}

	if err := s.SaveNotificationPrefs(ctx, prefs); err != nil {
		t.Fatalf("save prefs: %v", err)
	}
	got, err := s.GetNotificationPrefs(ctx, "u1")
	if err != nil {
		t.Fatalf("get prefs: %v", err)
	}
	if got != prefs {
		t.Fatalf("prefs = %+v, want %+v", got, prefs)
	}

	if _, err := s.GetNotificationPrefs(ctx, "u2"); err == nil {
		t.Fatal("expected an error for a user with no prefs")
	}
}

func TestListNotificationPrefsScansEveryUser(t *testing.T) {
	s, _ := newStore(t, 2) // small pages, so the scan paginator must loop
	ctx := context.Background()
	for _, u := range []string{"u3", "u1", "u2"} {
		if err := s.SaveNotificationPrefs(ctx, domain.NotificationPrefs{UserID: u, WAEnabled: true}); err != nil {
			t.Fatalf("save prefs %s: %v", u, err)
		}
	}
	// Entries and goals share the table; the scan filter must exclude them.
	save(t, s, entry(t, "e1", "2026-07-10", 100))
	if err := s.SaveGoal(ctx, domain.Goal{UserID: "u1", Month: "2026-07"}); err != nil {
		t.Fatalf("save goal: %v", err)
	}

	got, err := s.ListNotificationPrefs(ctx)
	if err != nil {
		t.Fatalf("list prefs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d prefs, want 3: %+v", len(got), got)
	}
	// Sorted by UserID so the notifier's output is deterministic.
	if got[0].UserID != "u1" || got[1].UserID != "u2" || got[2].UserID != "u3" {
		t.Fatalf("prefs order = %+v, want u1, u2, u3", got)
	}
}

func TestListNotificationPrefsPropagatesError(t *testing.T) {
	s, tbl := newStore(t, 0)
	boom := errors.New("scan failed")
	tbl.FailNext("Scan", boom)

	if _, err := s.ListNotificationPrefs(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

func TestNotificationLogDedupe(t *testing.T) {
	s, _ := newStore(t, 0)
	ctx := context.Background()

	sent, err := s.NotificationSent(ctx, "u1", "2026-07-20")
	if err != nil {
		t.Fatalf("notification sent: %v", err)
	}
	if sent {
		t.Fatal("nothing was recorded yet, so the key must read as unsent")
	}

	if err := s.RecordNotificationSent(ctx, "u1", "2026-07-20", time.Now()); err != nil {
		t.Fatalf("record notification: %v", err)
	}

	if sent, err = s.NotificationSent(ctx, "u1", "2026-07-20"); err != nil || !sent {
		t.Fatalf("sent = %v, err = %v — want the recorded key to read as sent", sent, err)
	}
	// A different day, and a different user, are separate keys.
	if sent, _ = s.NotificationSent(ctx, "u1", "2026-07-21"); sent {
		t.Fatal("a different day must not be deduped")
	}
	if sent, _ = s.NotificationSent(ctx, "u2", "2026-07-20"); sent {
		t.Fatal("a different user must not be deduped")
	}
}

func TestNotificationSentPropagatesError(t *testing.T) {
	s, tbl := newStore(t, 0)
	boom := errors.New("read failed")
	tbl.FailNext("GetItem", boom)

	if _, err := s.NotificationSent(context.Background(), "u1", "k"); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

// entryDiff reports the first field that differs between two entries, so a
// round-trip failure names the field instead of dumping two structs.
func entryDiff(want, got domain.FinancialEntry) string {
	dateStr := func(d *domain.CalendarDate) string {
		if d == nil {
			return "<nil>"
		}
		return d.String()
	}
	checks := []struct {
		field     string
		want, got string
	}{
		{"UserID", want.UserID, got.UserID},
		{"EntryID", string(want.EntryID), string(got.EntryID)},
		{"TransactionDate", want.TransactionDate.String(), got.TransactionDate.String()},
		{"DueDate", dateStr(want.DueDate), dateStr(got.DueDate)},
		{"PaymentDate", dateStr(want.PaymentDate), dateStr(got.PaymentDate)},
		{"Category", want.Category, got.Category},
		{"Description", want.Description, got.Description},
		{"Supplier", want.Supplier, got.Supplier},
		{"Type", string(want.Type), string(got.Type)},
		{"PaymentStatus", string(want.PaymentStatus), string(got.PaymentStatus)},
		{"Source", string(want.Source), string(got.Source)},
		{"CreatedAt", want.CreatedAt.UTC().Format(time.RFC3339), got.CreatedAt.UTC().Format(time.RFC3339)},
		{"UpdatedAt", want.UpdatedAt.UTC().Format(time.RFC3339), got.UpdatedAt.UTC().Format(time.RFC3339)},
		{"RecurrenceID", want.RecurrenceID, got.RecurrenceID},
	}
	for _, c := range checks {
		if c.want != c.got {
			return c.field + " = " + c.got + ", want " + c.want
		}
	}
	if want.Amount != got.Amount {
		return "Amount differs"
	}
	return ""
}
