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
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		entries:    make(map[string]domain.FinancialEntry),
		categories: make(map[string]domain.Category),
		goals:      make(map[string]domain.Goal),
		notifPrefs: make(map[string]domain.NotificationPrefs),
		notifLog:   make(map[string]struct{}),
	}
}

func entryKey(userID, entryID string) string { return userID + "#" + entryID }
func catKey(userID, slug string) string      { return userID + "#" + slug }
func goalKey(userID, month string) string    { return userID + "#" + month }
func notifLogKey(userID, key string) string  { return userID + "#" + key }

// --- Entries ---

func (s *InMemoryStore) SaveEntry(_ context.Context, entry domain.FinancialEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entryKey(entry.UserID, entry.EntryID)] = entry
	return nil
}

// SaveEntries writes all entries under a single lock, so readers never
// observe a partial series (mirrors the atomicity DynamoDBStore gets from
// TransactWriteItems).
func (s *InMemoryStore) SaveEntries(_ context.Context, entries []domain.FinancialEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		s.entries[entryKey(e.UserID, e.EntryID)] = e
	}
	return nil
}

func (s *InMemoryStore) GetEntry(_ context.Context, userID, entryID string) (domain.FinancialEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[entryKey(userID, entryID)]
	if !ok {
		return domain.FinancialEntry{}, fmt.Errorf("entry %q not found", entryID)
	}
	return e, nil
}

func (s *InMemoryStore) ListEntries(_ context.Context, userID string, filter EntryFilter) ([]domain.FinancialEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []domain.FinancialEntry
	for _, e := range s.entries {
		if e.UserID != userID {
			continue
		}

		// Cursor is an exclusive upper bound on the GSI2SK value.
		if filter.Cursor != "" {
			gsi2sk := effectiveDate(e).Format("2006-01-02") + "#" + e.EntryID
			if gsi2sk >= filter.Cursor {
				continue
			}
		}

		if filter.From != nil && effectiveDate(e).Before(*filter.From) {
			continue
		}
		if filter.To != nil && effectiveDate(e).After(*filter.To) {
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
		result = append(result, e)
	}

	sort.Slice(result, func(i, j int) bool {
		return effectiveDate(result[i]).After(effectiveDate(result[j]))
	})
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (s *InMemoryStore) UpdateEntry(_ context.Context, entry domain.FinancialEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := entryKey(entry.UserID, entry.EntryID)
	if _, ok := s.entries[key]; !ok {
		return fmt.Errorf("entry %q not found", entry.EntryID)
	}
	s.entries[key] = entry
	return nil
}

func (s *InMemoryStore) DeleteEntry(_ context.Context, userID, entryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := entryKey(userID, entryID)
	if _, ok := s.entries[key]; !ok {
		return fmt.Errorf("entry %q not found", entryID)
	}
	delete(s.entries, key)
	return nil
}

// --- Summaries ---

func (s *InMemoryStore) MonthlySummary(_ context.Context, userID, yearMonth string) (MonthlySummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summary := MonthlySummary{Month: yearMonth}
	for _, e := range s.entries {
		if e.UserID != userID {
			continue
		}
		if !strings.HasPrefix(effectiveDate(e).Format("2006-01"), yearMonth) {
			continue
		}
		if e.Type == domain.EntryTypeIncome {
			summary.TotalIncome += e.Amount
		} else {
			summary.TotalExpense += e.Amount
		}
	}
	summary.Balance = summary.TotalIncome - summary.TotalExpense
	return summary, nil
}

func (s *InMemoryStore) CategorySummary(_ context.Context, userID string, from, to time.Time) ([]CategorySummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totals := make(map[string]*CategorySummary)
	for _, e := range s.entries {
		if e.UserID != userID {
			continue
		}
		if effectiveDate(e).Before(from) || effectiveDate(e).After(to) {
			continue
		}
		key := e.Category
		if _, ok := totals[key]; !ok {
			totals[key] = &CategorySummary{Category: e.Category, Type: e.Type}
		}
		totals[key].Total += e.Amount
		totals[key].Count++
	}

	result := make([]CategorySummary, 0, len(totals))
	for _, v := range totals {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Total > result[j].Total
	})
	return result, nil
}

// CashFlowForecast projects daily running balance across the given calendar
// month (day 1 through the last day), not a rolling window centered on
// today — the dashboard always shows the current month.
func (s *InMemoryStore) CashFlowForecast(_ context.Context, userID, yearMonth string) ([]CashFlowPoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	from, err := time.Parse("2006-01", yearMonth)
	if err != nil {
		return nil, fmt.Errorf("invalid yearMonth %q: %w", yearMonth, err)
	}
	to := from.AddDate(0, 1, -1) // last day of the month
	days := int(to.Sub(from).Hours()/24) + 1

	// Aggregate entries by effective date
	type dayTotals struct {
		income  int64
		expense int64
	}
	byDay := make(map[string]*dayTotals)

	for _, e := range s.entries {
		if e.UserID != userID {
			continue
		}
		d := effectiveDate(e)
		if d.Before(from) || d.After(to) {
			continue
		}
		day := d.Format("2006-01-02")
		if _, ok := byDay[day]; !ok {
			byDay[day] = &dayTotals{}
		}
		if e.Type == domain.EntryTypeIncome {
			byDay[day].income += e.Amount
		} else {
			byDay[day].expense += e.Amount
		}
	}

	// Starting balance before "from"
	var running int64
	for _, e := range s.entries {
		if e.UserID != userID {
			continue
		}
		if !effectiveDate(e).Before(from) {
			continue
		}
		if e.Type == domain.EntryTypeIncome {
			running += e.Amount
		} else {
			running -= e.Amount
		}
	}

	points := make([]CashFlowPoint, 0, days)
	for i := 0; i < days; i++ {
		d := from.AddDate(0, 0, i)
		day := d.Format("2006-01-02")
		totals := byDay[day]
		var inc, exp int64
		if totals != nil {
			inc = totals.income
			exp = totals.expense
		}
		running += inc - exp
		points = append(points, CashFlowPoint{
			Date:             day,
			ProjectedIncome:  inc,
			ProjectedExpense: exp,
			RunningBalance:   running,
		})
	}
	return points, nil
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
