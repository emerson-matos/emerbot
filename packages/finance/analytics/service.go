package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// LedgerReader is the slice of the finance store the analysis reads. The
// assembly writes nothing, and saying so here keeps it independent of the
// 18-method Store — and lets a test double implement four methods.
type LedgerReader interface {
	ListEntries(ctx context.Context, userID string, filter pkgfinance.EntryFilter) ([]domain.FinancialEntry, error)
	MultiMonthlySummary(ctx context.Context, userID string, yearMonths []string) (map[string]pkgfinance.MonthlySummary, error)
	GetGoal(ctx context.Context, userID, month string) (domain.Goal, error)
	CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]pkgfinance.CashFlowPoint, error)
}

// Assemble fetches everything the analysis needs and builds it. This is the
// single entry point the dashboard API, the notifier and the AI bot all go
// through, so the three can never drift into telling the user different
// things about the same month.
//
// now must already be in the user's timezone — see Input.Now.
func Assemble(ctx context.Context, store LedgerReader, userID, month string, now time.Time) (Analysis, error) {
	// Parsed up front so every lookup below can assume a well-formed month,
	// including the trailing window MonthRange derives from it.
	if _, _, err := domain.ParseMonth(month); err != nil {
		return Analysis{}, err
	}
	months := MonthRange(month, HistoryMonths)

	entries, err := monthEntries(ctx, store, userID, month)
	if err != nil {
		return Analysis{}, err
	}

	previousMonth := months[len(months)-2]
	previousEntries, err := monthEntries(ctx, store, userID, previousMonth)
	if err != nil {
		return Analysis{}, err
	}

	summaryByMonth, err := store.MultiMonthlySummary(ctx, userID, months)
	if err != nil {
		return Analysis{}, fmt.Errorf("monthly summaries: %w", err)
	}
	summaries := make([]*pkgfinance.MonthlySummary, len(months))
	for i, m := range months {
		if s, ok := summaryByMonth[m]; ok {
			summaries[i] = &s
		}
	}

	// A month with no goal is the normal case, not a failure: GetGoal reports
	// "not found" as an error, and the analysis simply has no target to
	// measure against.
	goals := make([]*domain.Goal, len(months))
	for i, m := range months {
		if g, err := store.GetGoal(ctx, userID, m); err == nil {
			goals[i] = &g
		}
	}

	points, err := store.CashFlowForecast(ctx, userID, month)
	if err != nil {
		return Analysis{}, fmt.Errorf("cash flow forecast: %w", err)
	}

	return Build(Input{
		Month:           month,
		Entries:         entries,
		PreviousEntries: previousEntries,
		Summaries:       summaries,
		Goals:           goals,
		CashFlowPoints:  points,
		Now:             now,
	}), nil
}

func monthEntries(ctx context.Context, store LedgerReader, userID, month string) ([]domain.FinancialEntry, error) {
	from, to, err := domain.ParseMonth(month)
	if err != nil {
		return nil, err
	}
	entries, err := store.ListEntries(ctx, userID, pkgfinance.EntryFilter{From: &from, To: &to})
	if err != nil {
		return nil, fmt.Errorf("list entries for %s: %w", month, err)
	}
	return entries, nil
}
