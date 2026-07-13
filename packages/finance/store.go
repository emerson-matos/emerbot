package finance

import (
	"context"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// EntryFilter constrains ListEntries queries. Zero-value fields are ignored.
type EntryFilter struct {
	From     *time.Time
	To       *time.Time
	Category string
	Status   domain.PaymentStatus
	Type     domain.EntryType
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
	GetEntry(ctx context.Context, userID, entryID string) (domain.FinancialEntry, error)
	ListEntries(ctx context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error)
	UpdateEntry(ctx context.Context, entry domain.FinancialEntry) error
	DeleteEntry(ctx context.Context, userID, entryID string) error

	// Summaries
	MonthlySummary(ctx context.Context, userID, yearMonth string) (MonthlySummary, error)
	CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]CategorySummary, error)
	CashFlowForecast(ctx context.Context, userID string, days int) ([]CashFlowPoint, error)

	// Goals
	SaveGoal(ctx context.Context, goal domain.Goal) error
	GetGoal(ctx context.Context, userID, month string) (domain.Goal, error)

	// Categories
	SaveCategory(ctx context.Context, cat domain.Category) error
	ListCategories(ctx context.Context, userID string) ([]domain.Category, error)
}
