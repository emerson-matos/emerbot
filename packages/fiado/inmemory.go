package fiado

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// InMemoryStore is the caderninho without DynamoDB — the local stack's store,
// and the second implementation the conformance suite holds the DynamoDB one
// against.
//
// It keys its movements by the same sort keys the real table uses (see
// keys.go), so ordering and pagination are not a second implementation of the
// same idea: it sorts the strings DynamoDB would have sorted.
type InMemoryStore struct {
	mu    sync.RWMutex
	books map[string]*book
}

type book struct {
	debtors map[string]Debtor
	// movements is keyed by the client-ordered sort key, which addresses a
	// movement exactly as its Ref does.
	movements map[string]Movement
}

var _ Store = (*InMemoryStore)(nil)

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{books: map[string]*book{}}
}

func (s *InMemoryStore) bookOf(userID string) *book {
	b, ok := s.books[userID]
	if !ok {
		b = &book{debtors: map[string]Debtor{}, movements: map[string]Movement{}}
		s.books[userID] = b
	}
	return b
}

func (s *InMemoryStore) Record(ctx context.Context, m Movement) (Debtor, error) {
	if err := m.Validate(); err != nil {
		return Debtor{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.bookOf(m.UserID)
	b.movements[clientSK(m.Client, m.Date, m.ID)] = m

	d, ok := b.debtors[m.Client]
	if !ok {
		d = Debtor{UserID: m.UserID, Client: m.Client}
	}
	// Same three effects as the DynamoDB transaction: the name as last typed,
	// the balance moved by the signed amount, and Since seeded only when the
	// client was square.
	d.Name = m.Name
	d.Balance += m.Amount
	if d.Since == nil {
		date := m.Date
		d.Since = &date
	}
	if clearSince(d.Balance) {
		d.Since = nil
	}
	d.UpdatedAt = time.Now().UTC()
	b.debtors[m.Client] = d
	return d, nil
}

func (s *InMemoryStore) Delete(ctx context.Context, userID string, ref Ref) (Debtor, error) {
	if err := ref.Validate(); err != nil {
		return Debtor{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b := s.bookOf(userID)
	key := clientSK(ref.Client, ref.Date, ref.ID)
	m, ok := b.movements[key]
	if !ok {
		return Debtor{}, fmt.Errorf("%w: %q", ErrMovementNotFound, ref.ID)
	}
	delete(b.movements, key)

	d, ok := b.debtors[ref.Client]
	if !ok {
		return Debtor{}, fmt.Errorf("%w: %q", ErrDebtorNotFound, ref.Client)
	}
	d.Balance -= m.Amount
	switch {
	case clearSince(d.Balance):
		d.Since = nil
	case d.Since == nil:
		// The deletion re-opened a balance that had been settled — "o joão não
		// me pagou aquilo". The day it last left zero is not written down any
		// more, so it comes back from the movements, which are the truth. The
		// DynamoDB store does the same thing in settleSince.
		d.Since = sinceFromMovements(b.clientHistory(ref.Client))
	}
	d.UpdatedAt = time.Now().UTC()
	b.debtors[ref.Client] = d
	return d, nil
}

// clientHistory is one client's movements, oldest first — the order a balance
// replay has to walk them in.
func (b *book) clientHistory(client string) []Movement {
	prefix := clientQueryPrefix(client)
	keys := make([]string, 0, len(b.movements))
	for sk := range b.movements {
		if strings.HasPrefix(sk, prefix) {
			keys = append(keys, sk)
		}
	}
	sort.Strings(keys)
	out := make([]Movement, 0, len(keys))
	for _, sk := range keys {
		out = append(out, b.movements[sk])
	}
	return out
}

func (s *InMemoryStore) Debtor(ctx context.Context, userID, client string) (Debtor, error) {
	if client == "" {
		return Debtor{}, ErrNoClient
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.books[userID]
	if !ok {
		return Debtor{}, fmt.Errorf("%w: %q", ErrDebtorNotFound, client)
	}
	d, ok := b.debtors[client]
	if !ok {
		return Debtor{}, fmt.Errorf("%w: %q", ErrDebtorNotFound, client)
	}
	return d, nil
}

func (s *InMemoryStore) ListDebtors(ctx context.Context, userID string) ([]Debtor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.books[userID]
	if !ok {
		return nil, nil
	}
	out := make([]Debtor, 0, len(b.debtors))
	for _, d := range b.debtors {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Client < out[j].Client })
	return out, nil
}

func (s *InMemoryStore) DayMovements(ctx context.Context, userID string, date domain.CalendarDate, page Page) (MovementPage, error) {
	if !date.Valid() {
		return MovementPage{}, fmt.Errorf("data do dia é obrigatória")
	}
	return s.timeline(userID, dayQueryPrefix(date), func(m Movement) string {
		return daySK(m.Date, m.Client, m.ID)
	}, true, page)
}

func (s *InMemoryStore) ClientMovements(ctx context.Context, userID, client string, page Page) (MovementPage, error) {
	if client == "" {
		return MovementPage{}, ErrNoClient
	}
	return s.timeline(userID, clientQueryPrefix(client), func(m Movement) string {
		return clientSK(m.Client, m.Date, m.ID)
	}, false, page)
}

// timeline is both queries: the movements whose sort key starts with prefix,
// in key order (ascending for a day, descending for a client), cut at the
// cursor and the limit exactly as a DynamoDB Query would cut them.
func (s *InMemoryStore) timeline(userID, prefix string, keyOf func(Movement) string, ascending bool, page Page) (MovementPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	b, ok := s.books[userID]
	if !ok {
		return MovementPage{}, nil
	}

	type row struct {
		sk string
		m  Movement
	}
	rows := make([]row, 0, len(b.movements))
	for _, m := range b.movements {
		sk := keyOf(m)
		if strings.HasPrefix(sk, prefix) {
			rows = append(rows, row{sk: sk, m: m})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if ascending {
			return rows[i].sk < rows[j].sk
		}
		return rows[i].sk > rows[j].sk
	})

	if page.Cursor != "" {
		if !strings.HasPrefix(page.Cursor, prefix) {
			return MovementPage{}, fmt.Errorf("cursor %q não pertence a esta listagem", page.Cursor)
		}
		at := -1
		for i, r := range rows {
			if r.sk == page.Cursor {
				at = i
				break
			}
		}
		if at < 0 {
			return MovementPage{}, fmt.Errorf("cursor %q não corresponde a nenhum movimento", page.Cursor)
		}
		rows = rows[at+1:]
	}

	out := MovementPage{Movements: make([]Movement, 0, len(rows))}
	for i, r := range rows {
		if page.Limit > 0 && i >= page.Limit {
			out.NextCursor = rows[i-1].sk
			break
		}
		out.Movements = append(out.Movements, r.m)
	}
	return out, nil
}

// clearSince is the one rule for when a debtor stops having a "desde": there is
// no debt to date from. Non-positive, not just zero — a negative balance is the
// client's credit (they paid more than they owed), and "em aberto há 30 dias"
// printed over money the pharmacy owes *them* is the caderninho lying in the
// direction that makes somebody stop trusting it.
//
// Both stores go through it: the DynamoDB one after its atomic ADD, which
// cannot know the resulting balance while it is being written.
func clearSince(balance int64) bool { return balance <= 0 }
