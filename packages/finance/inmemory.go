package finance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// InMemoryStore implements Store for tests and local development without Docker.
type InMemoryStore struct {
	mu         sync.RWMutex
	entries    map[string]domain.FinancialEntry    // key: userID+entryID
	categories map[string]domain.Category          // key: userID+slug
	goals      map[string]domain.Goal              // key: userID+month
	notifPrefs map[string]domain.NotificationPrefs // key: userID
	notifLog   map[string]struct{}                 // key: userID+"#"+key
	insights   map[string]InsightSnapshot          // key: userID+"#"+date
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		entries:    make(map[string]domain.FinancialEntry),
		categories: make(map[string]domain.Category),
		goals:      make(map[string]domain.Goal),
		notifPrefs: make(map[string]domain.NotificationPrefs),
		notifLog:   make(map[string]struct{}),
		insights:   make(map[string]InsightSnapshot),
	}
}

func entryKey(userID, entryID string) string { return userID + "#" + entryID }
func catKey(userID, slug string) string      { return userID + "#" + slug }
func goalKey(userID, month string) string    { return userID + "#" + month }
func notifLogKey(userID, key string) string  { return userID + "#" + key }
func insightKey(userID, date string) string  { return userID + "#" + date }

// --- Entries ---

func (s *InMemoryStore) SaveEntry(_ context.Context, entry domain.FinancialEntry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entryKey(entry.UserID, string(entry.EntryID))] = entry
	return nil
}

// SaveEntries writes all entries under a single lock, so readers never
// observe a partial series (mirrors the atomicity DynamoDBStore gets from
// TransactWriteItems).
func (s *InMemoryStore) SaveEntries(_ context.Context, entries []domain.FinancialEntry) error {
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		s.entries[entryKey(e.UserID, string(e.EntryID))] = e
	}
	return nil
}

// GetEntry mirrors the DynamoDB store's key lookup, mismatched date included:
// there the date is half the sort key, so a wrong one addresses a row that does
// not exist. Ignoring it here would make the fake accept requests production
// rejects — the kind of divergence store_conformance_test.go exists to catch.
func (s *InMemoryStore) GetEntry(_ context.Context, userID string, date domain.CalendarDate, entryID string) (domain.FinancialEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[entryKey(userID, entryID)]
	if !ok || e.TransactionDate != date {
		return domain.FinancialEntry{}, fmt.Errorf("%w: %q on %s", ErrEntryNotFound, entryID, date)
	}
	return e, nil
}

func (s *InMemoryStore) FindEntryByID(_ context.Context, userID, entryID string) (domain.FinancialEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[entryKey(userID, entryID)]
	if !ok {
		return domain.FinancialEntry{}, fmt.Errorf("%w: %q", ErrEntryNotFound, entryID)
	}
	return e, nil
}

func (s *InMemoryStore) ListEntries(_ context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []domain.FinancialEntry
	for _, e := range s.entries {
		if e.UserID != userID {
			continue
		}

		// Cursor is an exclusive upper bound on the GSI2SK value.
		if filter.Cursor != "" {
			gsi2sk := EffectiveDate(e).Format("2006-01-02") + "#" + string(e.EntryID)
			if gsi2sk >= filter.Cursor {
				continue
			}
		}

		// The date range applies to whichever date the basis names, mirroring
		// the key condition the DynamoDB store pushes down. On the payment
		// basis an unpaid entry has no date at all and drops out entirely —
		// that is the sparse GSI1 index expressed in Go.
		date, ok := basisDate(filter.DateBasis, e)
		if !ok {
			continue
		}
		if filter.From != nil && date.Before(*filter.From) {
			continue
		}
		if filter.To != nil && date.After(*filter.To) {
			continue
		}
		if filter.Category != "" && e.Category != filter.Category {
			continue
		}
		if filter.Description != "" &&
			!strings.Contains(strings.ToLower(e.Description), strings.ToLower(filter.Description)) {
			continue
		}
		if filter.Status != "" && e.PaymentStatus != filter.Status {
			continue
		}
		if filter.Type != "" && e.Type != filter.Type {
			continue
		}
		if filter.Origin != "" && e.Origin != filter.Origin {
			continue
		}
		result = append(result, e)
	}

	sort.Slice(result, func(i, j int) bool {
		return sortDate(filter.DateBasis, result[i]).After(sortDate(filter.DateBasis, result[j]))
	})
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

// basisDate reports the date the given basis measures an entry by, and whether
// the entry is on that basis at all: a pending entry has no payment date, so it
// is absent from the cash basis rather than counted at some substitute date.
func basisDate(b DateBasis, e domain.FinancialEntry) (time.Time, bool) {
	switch b {
	case BasisPayment:
		cash := CashDate(e)
		if cash == nil {
			return time.Time{}, false
		}
		return *cash, true
	case BasisTransaction:
		return RevenueDate(e), true
	default:
		return EffectiveDate(e), true
	}
}

func (s *InMemoryStore) UpdateEntry(_ context.Context, previous, updated domain.FinancialEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := entryKey(updated.UserID, string(updated.EntryID))
	stored, ok := s.entries[key]
	if !ok || stored.TransactionDate != previous.TransactionDate {
		return fmt.Errorf("%w: %q", ErrEntryNotFound, updated.EntryID)
	}
	s.entries[key] = updated
	return nil
}

func (s *InMemoryStore) DeleteEntry(_ context.Context, userID string, date domain.CalendarDate, entryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := entryKey(userID, entryID)
	stored, ok := s.entries[key]
	if !ok || stored.TransactionDate != date {
		return fmt.Errorf("%w: %q on %s", ErrEntryNotFound, entryID, date)
	}
	delete(s.entries, key)
	return nil
}

// --- Summaries ---

// The summaries are derived views over ListEntries, shared with DynamoDBStore
// — see summaries.go. Deriving both from one implementation is what keeps the
// two Stores from answering the same question differently.

func (s *InMemoryStore) MonthlySummary(ctx context.Context, userID, yearMonth string) (MonthlySummary, error) {
	return monthlySummary(ctx, s, userID, yearMonth)
}

// MultiMonthlySummary returns one summary per requested month; months with no
// entries come back with zero totals rather than missing.
func (s *InMemoryStore) MultiMonthlySummary(ctx context.Context, userID string, yearMonths []string) (map[string]MonthlySummary, error) {
	return multiMonthlySummary(ctx, s, userID, yearMonths)
}

func (s *InMemoryStore) CategorySummary(ctx context.Context, userID string, from, to time.Time) ([]CategorySummary, error) {
	return categorySummary(ctx, s, userID, from, to)
}

func (s *InMemoryStore) CashFlowForecast(ctx context.Context, userID, yearMonth string) ([]CashFlowPoint, error) {
	return cashFlowForecast(ctx, s, userID, yearMonth)
}

// --- Goals ---

func (s *InMemoryStore) SaveGoal(_ context.Context, goal domain.Goal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goals[goalKey(goal.UserID, goal.Month)] = goal
	return nil
}

func (s *InMemoryStore) GetGoal(_ context.Context, userID, month string) (domain.Goal, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.goals[goalKey(userID, month)]
	if !ok {
		return domain.Goal{}, fmt.Errorf("goal not found for %s/%s", userID, month)
	}
	return g, nil
}

// --- Categories ---

func (s *InMemoryStore) SaveCategory(_ context.Context, cat domain.Category) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.categories[catKey(cat.UserID, cat.Slug)] = cat
	return nil
}

func (s *InMemoryStore) ListCategories(_ context.Context, userID string) ([]domain.Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []domain.Category
	for _, c := range s.categories {
		if c.UserID == userID {
			result = append(result, c)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Slug < result[j].Slug
	})
	return result, nil
}

// --- Notifications ---

func (s *InMemoryStore) SaveNotificationPrefs(_ context.Context, prefs domain.NotificationPrefs) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifPrefs[prefs.UserID] = prefs
	return nil
}

func (s *InMemoryStore) GetNotificationPrefs(_ context.Context, userID string) (domain.NotificationPrefs, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.notifPrefs[userID]
	if !ok {
		return domain.NotificationPrefs{}, fmt.Errorf("notification prefs not found for %s", userID)
	}
	return p, nil
}

func (s *InMemoryStore) ListNotificationPrefs(_ context.Context) ([]domain.NotificationPrefs, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]domain.NotificationPrefs, 0, len(s.notifPrefs))
	for _, p := range s.notifPrefs {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UserID < result[j].UserID })
	return result, nil
}

func (s *InMemoryStore) NotificationSent(_ context.Context, userID, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.notifLog[notifLogKey(userID, key)]
	return ok, nil
}

func (s *InMemoryStore) RecordNotificationSent(_ context.Context, userID, key string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifLog[notifLogKey(userID, key)] = struct{}{}
	return nil
}

// --- Insight snapshots ---

func (s *InMemoryStore) SaveInsightSnapshot(_ context.Context, userID, date string, snapshot []byte, computedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insights[insightKey(userID, date)] = InsightSnapshot{
		Snapshot:   snapshot,
		ComputedAt: computedAt,
	}
	return nil
}

func (s *InMemoryStore) GetInsightSnapshot(_ context.Context, userID, date string) (InsightSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.insights[insightKey(userID, date)]
	if !ok {
		return InsightSnapshot{}, fmt.Errorf("insight snapshot not found for %s/%s", userID, date)
	}
	return snap, nil
}
