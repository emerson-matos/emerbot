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

func TestInMemoryStoreMonthlySummaryAndCategorySummary(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	for _, entry := range []domain.FinancialEntry{
		testEntry("u1", "e1", "2026-07-01", 10000, "aluguel", domain.EntryTypeExpense),
		testEntry("u1", "e2", "2026-07-05", 35000, "venda_balcao", domain.EntryTypeIncome),
		testEntry("u1", "e3", "2026-07-07", 5000, "aluguel", domain.EntryTypeExpense),
		testEntry("u1", "e4", "2026-06-30", 9999, "aluguel", domain.EntryTypeExpense),
	} {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	monthly, err := store.MonthlySummary(ctx, "u1", "2026-07")
	if err != nil {
		t.Fatalf("MonthlySummary: %v", err)
	}
	if monthly.TotalIncome != 35000 || monthly.TotalExpense != 15000 || monthly.Balance != 20000 {
		t.Fatalf("unexpected monthly summary: %+v", monthly)
	}

	from := mustDate("2026-07-01")
	to := mustDate("2026-07-31")
	categories, err := store.CategorySummary(ctx, "u1", from, to)
	if err != nil {
		t.Fatalf("CategorySummary: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("expected 2 category rows, got %d", len(categories))
	}
	if categories[0].Category != "venda_balcao" || categories[0].Total != 35000 {
		t.Fatalf("expected top category to be venda_balcao, got %+v", categories[0])
	}
	if categories[1].Category != "aluguel" || categories[1].Total != 15000 || categories[1].Count != 2 {
		t.Fatalf("unexpected aluguel category summary: %+v", categories[1])
	}
}

func TestInMemoryStoreMonthlySummaryBucketsByDueDateNotRegistrationDate(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	// A /recorrente-style pending expense: registered in July but due in
	// September — it must count toward September's totals, not July's.
	dueInSeptember := domain.NewCalendarDate(mustDate("2026-09-10"))
	installment := testEntry("u1", "e1", "2026-07-14", 35000, "aluguel", domain.EntryTypeExpense)
	installment.PaymentStatus = domain.PaymentStatusPending
	installment.PaymentDate = nil
	installment.DueDate = &dueInSeptember

	// An already-settled July expense (no DueDate) still counts toward July.
	settled := testEntry("u1", "e2", "2026-07-05", 5000, "aluguel", domain.EntryTypeExpense)

	for _, entry := range []domain.FinancialEntry{installment, settled} {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	july, err := store.MonthlySummary(ctx, "u1", "2026-07")
	if err != nil {
		t.Fatalf("MonthlySummary(2026-07): %v", err)
	}
	if july.TotalExpense != 5000 {
		t.Fatalf("expected July expense to exclude the September-due installment, got %+v", july)
	}

	september, err := store.MonthlySummary(ctx, "u1", "2026-09")
	if err != nil {
		t.Fatalf("MonthlySummary(2026-09): %v", err)
	}
	if september.TotalExpense != 35000 {
		t.Fatalf("expected September expense to include the pending installment (regardless of it not being due yet), got %+v", september)
	}
}

func TestInMemoryStoreCashFlowForecastCoversWholeCalendarMonth(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	ctx := context.Background()

	beforeMonth := mustDate("2026-06-28")
	day1Due := domain.NewCalendarDate(mustDate("2026-07-01"))
	day2Due := domain.NewCalendarDate(mustDate("2026-07-02"))

	pastIncome := testEntryAt("u1", "past-income", beforeMonth, 50000, "venda_balcao", domain.EntryTypeIncome)
	day1Income := testEntryAt("u1", "day1-income", mustDate("2026-07-01"), 15000, "venda_balcao", domain.EntryTypeIncome)
	day1Income.DueDate = &day1Due
	day2Expense := testEntryAt("u1", "day2-expense", mustDate("2026-07-01"), 7000, "energia_agua", domain.EntryTypeExpense)
	day2Expense.DueDate = &day2Due

	for _, entry := range []domain.FinancialEntry{pastIncome, day1Income, day2Expense} {
		if err := store.SaveEntry(ctx, entry); err != nil {
			t.Fatalf("SaveEntry(%s): %v", entry.EntryID, err)
		}
	}

	points, err := store.CashFlowForecast(ctx, "u1", "2026-07")
	if err != nil {
		t.Fatalf("CashFlowForecast: %v", err)
	}
	if len(points) != 31 {
		t.Fatalf("expected 31 points (July has 31 days), got %d", len(points))
	}
	if points[0].Date != "2026-07-01" || points[len(points)-1].Date != "2026-07-31" {
		t.Fatalf("expected points to span the full month, got %s..%s", points[0].Date, points[len(points)-1].Date)
	}

	day1, day2 := points[0], points[1]
	// The pre-month entry seeds the starting balance exactly once — it must
	// not also land in (or double-count against) day 1's own totals.
	if day1.ProjectedIncome != 15000 || day1.RunningBalance != 65000 {
		t.Fatalf("unexpected day 1 point: %+v", day1)
	}
	if day2.ProjectedExpense != 7000 || day2.RunningBalance != 58000 {
		t.Fatalf("unexpected day 2 point: %+v", day2)
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
