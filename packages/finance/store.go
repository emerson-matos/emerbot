package finance

import (
	"context"
	"fmt"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// DateBasis selects which of an entry's three dates From/To bound. The three
// exist because the metrics genuinely disagree about when money counts:
// a sale counts when it is made, cash counts when it lands, and a bill counts
// when it falls due.
type DateBasis string

const (
	// BasisEffective bounds EffectiveDate — the default, and the only basis
	// that supports Cursor and Limit (see EntryFilter). It answers "what falls
	// in this month", pending bills and receivables included.
	BasisEffective DateBasis = ""
	// BasisTransaction bounds TransactionDate: the day the sale or expense
	// actually happened. This is the faturamento basis — a crediário sale made
	// in January is January's revenue even though it is due in February.
	BasisTransaction DateBasis = "transaction"
	// BasisPayment bounds PaymentDate: the day money actually moved. This is
	// the entradas/saídas de caixa basis. Pending entries have no PaymentDate
	// and are absent from this basis entirely, which is the point — money that
	// has not arrived is not cash.
	BasisPayment DateBasis = "payment"
)

// EntryFilter constrains ListEntries queries. Zero-value fields are ignored.
// From/To bound the date named by DateBasis, defaulting to EffectiveDate (see
// below) rather than the registration Date.
type EntryFilter struct {
	From *time.Time
	To   *time.Time
	// DateBasis selects which date From/To bound. The zero value keeps the
	// historical behaviour (EffectiveDate).
	DateBasis DateBasis
	Category  string
	// Origin keeps only income entries with this origin. Faturamento is
	// domain.OriginVenda; see domain.IsRevenue.
	Origin domain.IncomeOrigin
	// Description, when set, keeps only entries whose Description contains this
	// substring (case-insensitive). Used by the GeminiAgent's search_entries
	// tool to answer free-text lookups ("quanto paguei de aluguel?").
	Description string
	Status      domain.PaymentStatus
	Type        domain.EntryType
	// Cursor is an exclusive upper bound in the form "YYYY-MM-DD#EntryID",
	// matching the GSI2SK format. When set, ListEntries returns only entries
	// with GSI2SK < Cursor, most-recent first. This avoids the page-boundary
	// data loss that happens when cursor-based pagination subtracts a day
	// from EffectiveDate (entries sharing the same EffectiveDate across a
	// page boundary would be silently skipped).
	//
	// Only valid with BasisEffective: the cursor is shaped like GSI2SK, so
	// comparing it against any other basis's sort key paginates against the
	// wrong ordering and drops rows without an error. ListEntries rejects the
	// combination rather than trusting callers to remember.
	Cursor string
	// Limit caps the number of entries returned, most-recent (by
	// EffectiveDate) first. Zero means "no cap" — callers that page through
	// results (see apps/dashboard-api/internal/finance/entries.go's List
	// handler) should always set this rather than relying on From/To alone,
	// to bound DynamoDB read cost and response size.
	//
	// Only valid with BasisEffective, for the same reason as Cursor: "the
	// most-recent N" is meaningless until we agree which date orders them.
	Limit int
}

// Validate reports whether the filter's fields can be honoured together.
// Cursor and Limit both mean "the most recent N by EffectiveDate", which no
// other basis can order — silently returning a truncated page against the
// wrong sort key would lose money from a financial view with no error to show
// for it, so this is a hard failure rather than a documented footgun.
func (f EntryFilter) Validate() error {
	if f.DateBasis == BasisEffective {
		return nil
	}
	if f.Cursor != "" {
		return fmt.Errorf("cursor is only supported on the effective-date basis, got %q", f.DateBasis)
	}
	if f.Limit > 0 {
		return fmt.Errorf("limit is only supported on the effective-date basis, got %q", f.DateBasis)
	}
	return nil
}

// EffectiveDate is the date an entry counts toward for monthly/period views
// (ListEntries date range, MonthlySummary, CategorySummary, CashFlowForecast):
// DueDate when set, since a pending bill or receivable belongs to the month
// it's due — whether or not that day has passed — not the month it happened
// to be registered in. Falls back to Date for already-settled entries, which
// have no DueDate.
// It is exported because packages/finance/analytics has to bucket entries by
// the same date the summaries do — comparing two months by any other date
// would put an entry in one bucket here and another there.
func EffectiveDate(e domain.FinancialEntry) time.Time {
	if e.DueDate != nil {
		return e.DueDate.Time()
	}
	return e.TransactionDate.Time()
}

// RevenueDate is the date an entry counts toward faturamento: the day the sale
// happened, never the day it was paid or is due. A crediário sale made on
// January 5th and due February 5th is January's revenue — the pharmacy sold
// something in January.
func RevenueDate(e domain.FinancialEntry) time.Time {
	return e.TransactionDate.Time()
}

// CashDate is the date money actually moved, or nil when it has not moved yet.
// domain.Normalize guarantees a paid entry has a PaymentDate and a pending one
// does not, so nil here means "still pending" — which is why pending entries
// are absent from the cash basis rather than counted at some other date.
func CashDate(e domain.FinancialEntry) *time.Time {
	if e.PaymentStatus != domain.PaymentStatusPaid || e.PaymentDate == nil {
		return nil
	}
	t := e.PaymentDate.Time()
	return &t
}

// MonthlySummary aggregates a calendar month's totals. Every amount is
// centavos.
//
// The three inflow figures answer three different questions and are bucketed
// by three different dates on purpose — that is the whole point of the split,
// and collapsing any two of them back together is how this codebase got a
// loan counted as business growth twice before (see docs/adr/ADR-016).
type MonthlySummary struct {
	Month string // "2026-07"
	// TotalRevenue is FATURAMENTO: what the pharmacy sold, by RevenueDate,
	// paid or not. Every performance indicator is measured against this one —
	// goals, projections, period comparisons, growth, the AI's insights.
	TotalRevenue int64
	// TotalCashIn is ENTRADAS DE CAIXA: money that actually arrived, by
	// CashDate, whatever its origin. A loan is here and not in TotalRevenue.
	// Used for cash-flow and liquidity readings, never for performance.
	TotalCashIn int64
	// TotalCashOut is money that actually left, by CashDate. It pairs with
	// TotalCashIn so callers can read real cash movement without mixing bases.
	TotalCashOut int64
	// TotalExpectedIn is every inflow by EffectiveDate, pending receivables
	// included — what the month is *expected* to bring in. Pairs with
	// TotalExpense, which is on the same basis, to make ExpectedBalance.
	TotalExpectedIn int64
	// TotalExpense is every outflow by EffectiveDate, pending bills included.
	TotalExpense int64
	// ExpectedBalance is TotalExpectedIn - TotalExpense: both sides on the
	// EffectiveDate basis, so it is a forecast of the month, not a cash
	// position. For cash, read TotalCashIn - TotalCashOut.
	ExpectedBalance int64
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

// InsightSnapshot is a cached daily analysis persisted by the notifier as a
// subproduct of its daily digest run. The dashboard-api reads it instead of
// recomputing on every request.
type InsightSnapshot struct {
	Snapshot   []byte    // JSON-serialized analytics.Analysis
	ComputedAt time.Time // when the analysis was computed
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
	// MultiMonthlySummary returns one summary per requested month, keyed by
	// "YYYY-MM". Months with no entries are present with zero totals rather
	// than absent, so callers can index the result directly. It exists so the
	// analysis's trailing-month window costs one query instead of one per
	// month.
	MultiMonthlySummary(ctx context.Context, userID string, yearMonths []string) (map[string]MonthlySummary, error)
	CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]CategorySummary, error)
	CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]CashFlowPoint, error)

	// Goals
	SaveGoal(ctx context.Context, goal domain.Goal) error
	GetGoal(ctx context.Context, userID, month string) (domain.Goal, error)

	// Categories
	SaveCategory(ctx context.Context, cat domain.Category) error
	ListCategories(ctx context.Context, userID string) ([]domain.Category, error)

	// Notification preferences
	SaveNotificationPrefs(ctx context.Context, prefs domain.NotificationPrefs) error
	GetNotificationPrefs(ctx context.Context, userID string) (domain.NotificationPrefs, error)
	// ListNotificationPrefs returns every user's prefs — used by the scheduled
	// notifier, which has no per-user request context to key off of.
	ListNotificationPrefs(ctx context.Context) ([]domain.NotificationPrefs, error)

	// Notification delivery log — lets the notifier avoid re-sending the same
	// alert to the same user twice. key is caller-defined (e.g. "2026-07-20").
	NotificationSent(ctx context.Context, userID, key string) (bool, error)
	RecordNotificationSent(ctx context.Context, userID, key string, sentAt time.Time) error

	// Insight snapshots — daily cache of the analysis, persisted by the
	// notifier as a subproduct of its digest run. date is "YYYY-MM-DD".
	SaveInsightSnapshot(ctx context.Context, userID, date string, snapshot []byte, computedAt time.Time) error
	GetInsightSnapshot(ctx context.Context, userID, date string) (InsightSnapshot, error)
}
