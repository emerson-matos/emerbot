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
	entries    map[string]domain.FinancialEntry // key: userID+entryID
	categories map[string]domain.Category       // key: userID+slug
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		entries:    make(map[string]domain.FinancialEntry),
		categories: make(map[string]domain.Category),
	}
}

func entryKey(userID, entryID string) string { return userID + "#" + entryID }
func catKey(userID, slug string) string       { return userID + "#" + slug }

// --- Entries ---

func (s *InMemoryStore) SaveEntry(_ context.Context, entry domain.FinancialEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entryKey(entry.UserID, entry.EntryID)] = entry
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
		if filter.From != nil && e.Date.Before(*filter.From) {
			continue
		}
		if filter.To != nil && e.Date.After(*filter.To) {
			continue
		}
		if filter.Category != "" && e.Category != filter.Category {
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
		return result[i].Date.After(result[j].Date)
	})
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
		if !strings.HasPrefix(e.Date.Format("2006-01"), yearMonth) {
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
		if e.Date.Before(from) || e.Date.After(to) {
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

func (s *InMemoryStore) CashFlowForecast(_ context.Context, userID string, days int) ([]CashFlowPoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	past := days / 2
	future := days - past

	from := today.AddDate(0, 0, -past)
	to := today.AddDate(0, 0, future-1)

	// Aggregate pending entries by due date
	type dayTotals struct {
		income  int64
		expense int64
	}
	byDay := make(map[string]*dayTotals)

	for _, e := range s.entries {
		if e.UserID != userID {
			continue
		}
		dueDate := e.DueDate
		if dueDate == nil {
			d := e.Date
			dueDate = &d
		}
		if dueDate.Before(from) || dueDate.After(to) {
			continue
		}
		day := dueDate.Format("2006-01-02")
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
		d := e.Date
		if !d.Before(from) {
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
