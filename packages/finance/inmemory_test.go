package finance

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func TestInMemoryStoreListEntriesAppliesFiltersAndSortsDesc(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	entry1 := testEntry("u1", "e1", "2026-07-10", 10000, "aluguel", domain.EntryTypeExpense)
	entry2 := testEntry("u1", "e2", "2026-07-12", 20000, "venda_balcao", domain.EntryTypeIncome)
	entry2.PaymentStatus = domain.PaymentStatusPending
	entry2.PaymentDate = nil
	entry3 := testEntry("u1", "e3", "2026-07-11", 15000, "aluguel", domain.EntryTypeExpense)
	otherUser := testEntry("u2", "e4", "2026-07-13", 5000, "aluguel", domain.EntryTypeExpense)

	for _, entry := range []domain.FinancialEntry{entry1, entry2, entry3, otherUser} {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	from := mustDate("2026-07-10")
	to := mustDate("2026-07-12")
	entries, err := store.ListEntries(ctx, "u1", EntryFilter{
		From:     &from,
		To:       &to,
		Category: "aluguel",
		Type:     domain.EntryTypeExpense,
	})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].EntryID != "e3" || entries[1].EntryID != "e1" {
		t.Fatalf("expected entries sorted by date desc, got %s then %s", entries[0].EntryID, entries[1].EntryID)
	}
}

func TestInMemoryStoreListEntriesRespectsLimit(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	entry1 := testEntry("u1", "e1", "2026-07-10", 10000, "aluguel", domain.EntryTypeExpense)
	entry2 := testEntry("u1", "e2", "2026-07-12", 20000, "venda_balcao", domain.EntryTypeIncome)
	entry3 := testEntry("u1", "e3", "2026-07-11", 15000, "aluguel", domain.EntryTypeExpense)

	for _, entry := range []domain.FinancialEntry{entry1, entry2, entry3} {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	entries, err := store.ListEntries(ctx, "u1", EntryFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected Limit:2 to cap results at 2, got %d", len(entries))
	}
	// Most-recent-first: e2 (07-12) then e3 (07-11), e1 (07-10) dropped.
	if entries[0].EntryID != "e2" || entries[1].EntryID != "e3" {
		t.Fatalf("expected the 2 most recent entries (e2, e3), got %s then %s", entries[0].EntryID, entries[1].EntryID)
	}

	unlimited, err := store.ListEntries(ctx, "u1", EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(unlimited) != 3 {
		t.Fatalf("expected Limit:0 (zero value) to mean unbounded, got %d entries", len(unlimited))
	}
}

func TestInMemoryStoreSaveEntriesPersistsWholeBatch(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	batch := []domain.FinancialEntry{
		testEntry("u1", "e1", "2026-07-01", 10000, "aluguel", domain.EntryTypeExpense),
		testEntry("u1", "e2", "2026-08-01", 10000, "aluguel", domain.EntryTypeExpense),
		testEntry("u1", "e3", "2026-09-01", 10000, "aluguel", domain.EntryTypeExpense),
	}
	if err := store.SaveEntries(ctx, batch); err != nil {
		t.Fatalf("SaveEntries: %v", err)
	}

	entries, err := store.ListEntries(ctx, "u1", EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestInMemoryStoreGoalAndCategoryLifecycle(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	goal := domain.Goal{UserID: "u1", Month: "2026-07", RevenueTarget: 100000, ExpenseTarget: 40000}
	if err := store.SaveGoal(ctx, goal); err != nil {
		t.Fatalf("SaveGoal: %v", err)
	}
	gotGoal, err := store.GetGoal(ctx, "u1", "2026-07")
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if gotGoal != goal {
		t.Fatalf("expected saved goal, got %+v", gotGoal)
	}

	cat1 := domain.Category{UserID: "u1", Slug: "energia_agua", Label: "Energia / Agua", Type: domain.EntryTypeExpense}
	cat2 := domain.Category{UserID: "u1", Slug: "venda_balcao", Label: "Venda Balcao", Type: domain.EntryTypeIncome}
	if err := store.SaveCategory(ctx, cat1); err != nil {
		t.Fatalf("SaveCategory cat1: %v", err)
	}
	if err := store.SaveCategory(ctx, cat2); err != nil {
		t.Fatalf("SaveCategory cat2: %v", err)
	}

	categories, err := store.ListCategories(ctx, "u1")
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(categories))
	}
	if categories[0].Slug != "energia_agua" || categories[1].Slug != "venda_balcao" {
		t.Fatalf("expected categories sorted by slug, got %+v", categories)
	}
}

func TestInMemoryStorePreservesPaymentDate(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	payDate := domain.NewCalendarDate(mustDate("2026-07-10"))
	entry := testEntry("u1", "e1", "2026-07-10", 10000, "aluguel", domain.EntryTypeExpense)
	entry.PaymentDate = &payDate

	if err := store.SaveEntry(ctx, entry); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	entries, err := store.ListEntries(ctx, "u1", EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].PaymentDate == nil {
		t.Fatal("expected PaymentDate to survive save/list round-trip")
	}
	if !(*entries[0].PaymentDate).Equal(payDate) {
		t.Fatalf("expected PaymentDate %v, got %v", payDate, *entries[0].PaymentDate)
	}
}

func TestInMemoryStoreUpdateAndDeleteMissingEntry(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()
	entry := testEntry("u1", "missing", "2026-07-10", 10000, "aluguel", domain.EntryTypeExpense)

	if err := store.UpdateEntry(ctx, entry); err == nil {
		t.Fatal("expected UpdateEntry to fail for missing entry")
	}
	if err := store.DeleteEntry(ctx, "u1", "missing"); err == nil {
		t.Fatal("expected DeleteEntry to fail for missing entry")
	}
	if _, err := store.GetGoal(ctx, "u1", "2026-07"); err == nil {
		t.Fatal("expected GetGoal to fail for missing goal")
	}
}

func testEntry(userID, entryID, date string, amount int64, category string, entryType domain.EntryType) domain.FinancialEntry {
	return testEntryAt(userID, entryID, mustDate(date), amount, category, entryType)
}

func testEntryAt(userID, entryID string, date time.Time, amount int64, category string, entryType domain.EntryType) domain.FinancialEntry {
	cd := domain.NewCalendarDate(date)
	entry := domain.FinancialEntry{
		UserID:          userID,
		EntryID:         domain.EntryID(entryID),
		TransactionDate: cd,
		Amount:          amount,
		Category:        category,
		Type:            entryType,
		Description:     category,
		PaymentStatus:   domain.PaymentStatusPaid,
		Source:          domain.SourceManual,
		CreatedAt:       date,
		UpdatedAt:       date,
	}
	payDate := cd
	entry.PaymentDate = &payDate
	return entry
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestInMemoryStoreMultiMonthlySummary(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	entries := []domain.FinancialEntry{
		testEntry("u1", "e1", "2026-05-10", 10000, "venda_balcao", domain.EntryTypeIncome),
		testEntry("u1", "e2", "2026-07-03", 30000, "venda_balcao", domain.EntryTypeIncome),
		testEntry("u1", "e3", "2026-07-04", 12000, "aluguel", domain.EntryTypeExpense),
		// Outside the requested window, and another user's row inside it —
		// neither may reach the totals.
		testEntry("u1", "e4", "2026-04-01", 99999, "aluguel", domain.EntryTypeExpense),
		testEntry("u2", "e5", "2026-07-05", 88888, "aluguel", domain.EntryTypeExpense),
	}
	for _, entry := range entries {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	got, err := store.MultiMonthlySummary(ctx, "u1", []string{"2026-05", "2026-06", "2026-07"})
	if err != nil {
		t.Fatalf("MultiMonthlySummary: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d summaries, want one per requested month", len(got))
	}
	// A month with no entries is present with zero totals, not missing.
	if june, ok := got["2026-06"]; !ok || june.Balance != 0 || june.Month != "2026-06" {
		t.Errorf("2026-06 = %+v (present=%t), want a zeroed summary", june, ok)
	}
	if may := got["2026-05"]; may.TotalIncome != 10000 || may.Balance != 10000 {
		t.Errorf("2026-05 = %+v, want 10000 income", may)
	}
	july := got["2026-07"]
	if july.TotalIncome != 30000 || july.TotalExpense != 12000 || july.Balance != 18000 {
		t.Errorf("2026-07 = %+v, want 30000/12000/18000", july)
	}
}

func TestInMemoryStoreMultiMonthlySummaryRejectsBadMonths(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	if _, err := store.MultiMonthlySummary(context.Background(), "u1", []string{"julho"}); err == nil {
		t.Error("expected an error for a malformed month")
	}
}

func TestInMemoryStoreMultiMonthlySummaryWithoutMonths(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	got, err := store.MultiMonthlySummary(context.Background(), "u1", nil)
	if err != nil {
		t.Fatalf("MultiMonthlySummary: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want an empty map", got)
	}
}
