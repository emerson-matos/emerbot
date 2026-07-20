package finance

import (
	"context"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// EntryFilter constrains ListEntries queries. Zero-value fields are ignored.
// From/To bound an entry's effectiveDate (see below), not necessarily its
// registration Date.
type EntryFilter struct {
	From     *time.Time
	To       *time.Time
	Category string
	Status   domain.PaymentStatus
	Type     domain.EntryType
	// Limit caps the number of entries returned, most-recent (by
	// effectiveDate) first. Zero means "no cap" — callers that page through
	// results (see apps/dashboard-api/internal/finance/entries.go's List
	// handler) should always set this rather than relying on From/To alone,
	// to bound DynamoDB read cost and response size.
	Limit int
}

// effectiveDate is the date an entry counts toward for monthly/period views
// (ListEntries date range, MonthlySummary, CategorySummary, CashFlowForecast):
// DueDate when set, since a pending bill or receivable belongs to the month
// it's due — whether or not that day has passed — not the month it happened
// to be registered in. Falls back to Date for already-settled entries, which
// have no DueDate.
func effectiveDate(e domain.FinancialEntry) time.Time {
	if e.DueDate != nil {
		return *e.DueDate
	}
	return e.Date
}

// MonthlySummary aggregates income and expense totals for a calendar month.
type MonthlySummary struct {
	Month        string // "2026-07"
	TotalIncome  int64  // centavos
	TotalExpense int64  // centavos
	Balance      int64  // TotalIncome - TotalExpense
}

// CategorySummary aggregates totals per category for a date range.
type CategorySummary struct {
	Category string
	Label    string
	Type     domain.EntryType
	Total    int64 // centavos
	Count    int
}

// CashFlowPoint represents a single day in the 30-day cash flow projection.
type CashFlowPoint struct {
	Date             string // "2026-07-12"
	ProjectedIncome  int64  // centavos — income expected on this date
	ProjectedExpense int64  // centavos — expenses expected on this date
	RunningBalance   int64  // cumulative balance up to and including this date
}

// Store defines all persistence operations for financial data.
type Store interface {
	// Entries
	SaveEntry(ctx context.Context, entry domain.FinancialEntry) error
	// SaveEntries persists multiple entries as one or more atomic writes (see
	// DynamoDBStore.SaveEntries for the chunking caveat above 100 entries).
	// Used by /recorrente to create a whole recurrence series together.
	SaveEntries(ctx context.Context, entries []domain.FinancialEntry) error
	GetEntry(ctx context.Context, userID, entryID string) (domain.FinancialEntry, error)
	ListEntries(ctx context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error)
	UpdateEntry(ctx context.Context, entry domain.FinancialEntry) error
	DeleteEntry(ctx context.Context, userID, entryID string) error

	// Summaries
	MonthlySummary(ctx context.Context, userID, yearMonth string) (MonthlySummary, error)
	CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]CategorySummary, error)
	CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]CashFlowPoint, error)

	// Goals
	SaveGoal(ctx context.Context, goal domain.Goal) error
	GetGoal(ctx context.Context, userID, month string) (domain.Goal, error)

	// Categories
	SaveCategory(ctx context.Context, cat domain.Category) error
	ListCategories(ctx context.Context, userID string) ([]domain.Category, error)
}
