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

	entries, err := monthEntries(ctx, store, userID, month, pkgfinance.BasisEffective)
	if err != nil {
		return Analysis{}, err
	}
	// A second read on the transaction basis, because faturamento is measured
	// on the day of the sale and the effective-date read above cannot see it: a
	// crediário sale made this month but due next is absent from `entries`
	// entirely. Computing revenue from `entries` would put the same sale in the
	// KPI of one month and the goal of another.
	revenueEntries, err := monthEntries(ctx, store, userID, month, pkgfinance.BasisTransaction)
	if err != nil {
		return Analysis{}, err
	}

	previousMonth := months[len(months)-2]
	previousEntries, err := monthEntries(ctx, store, userID, previousMonth, pkgfinance.BasisEffective)
	if err != nil {
		return Analysis{}, err
	}
	previousRevenueEntries, err := monthEntries(ctx, store, userID, previousMonth, pkgfinance.BasisTransaction)
	if err != nil {
		return Analysis{}, err
	}

	// The trailing window is not month-aligned and reaches back past the
	// previous month, so neither read above covers it. Both bases are needed and
	// for different things: transaction prices the revenue projection, effective
	// prices the cash runway (see Input.WindowEntries).
	//
	// Two Queries, and cheap ones: each range goes straight into a BETWEEN on
	// the sort key of its own basis — the base table for transaction, GSI2 for
	// effective — so a window crossing month boundaries costs the same as a
	// single month. Folding the five entry reads into two contiguous ones is a
	// real saving and a separate change; it rewrites Input and every test around
	// it.
	windowRevenueEntries, windowEntries, err := windowReads(ctx, store, userID, month, now)
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
		Month:                  month,
		Entries:                entries,
		PreviousEntries:        previousEntries,
		RevenueEntries:         revenueEntries,
		PreviousRevenueEntries: previousRevenueEntries,
		WindowRevenueEntries:   windowRevenueEntries,
		WindowEntries:          windowEntries,
		Summaries:              summaries,
		Goals:                  goals,
		CashFlowPoints:         points,
		Now:                    now,
	}), nil
}

func monthEntries(ctx context.Context, store LedgerReader, userID, month string, basis pkgfinance.DateBasis) ([]domain.FinancialEntry, error) {
	from, to, err := domain.ParseMonth(month)
	if err != nil {
		return nil, err
	}
	entries, err := rangeEntries(ctx, store, userID, from, to, basis)
	if err != nil {
		return nil, fmt.Errorf("month %s: %w", month, err)
	}
	return entries, nil
}

// windowReads fetches the trailing window on both date bases: faturamento by
// the day of the sale, for the revenue projection, and everything by the day it
// lands, for the cash runway.
//
// A closed month has no day left to price, so it does not pay for the window at
// all — and every month but the current one is closed, which is most of what the
// dashboard asks for when someone browses back through the year. buildProjection
// decides "fechado" before consulting the rates, so skipping the reads here
// cannot turn into a "sem_base" label.
func windowReads(ctx context.Context, store LedgerReader, userID, month string, now time.Time) (revenue, cash []domain.FinancialEntry, err error) {
	clock := newMonthClock(month, now)
	if !clock.inProgress {
		return nil, nil, nil
	}
	from, to := clock.projectionWindow()
	revenue, err = rangeEntries(ctx, store, userID, from.Time(), to.Time(), pkgfinance.BasisTransaction)
	if err != nil {
		return nil, nil, err
	}
	cash, err = rangeEntries(ctx, store, userID, from.Time(), to.Time(), pkgfinance.BasisEffective)
	if err != nil {
		return nil, nil, err
	}
	return revenue, cash, nil
}

// rangeEntries reads an arbitrary span of days, both ends inclusive. The
// projection's window is not month-shaped, and the store pushes From/To into the
// sort key of whichever basis is asked for, so a span crossing months is a
// single Query rather than one per month.
func rangeEntries(ctx context.Context, store LedgerReader, userID string, from, to time.Time, basis pkgfinance.DateBasis) ([]domain.FinancialEntry, error) {
	entries, err := store.ListEntries(ctx, userID, pkgfinance.EntryFilter{From: &from, To: &to, DateBasis: basis})
	if err != nil {
		return nil, fmt.Errorf("list entries %s..%s (%s basis): %w",
			from.Format("2006-01-02"), to.Format("2006-01-02"), basis, err)
	}
	return entries, nil
}
