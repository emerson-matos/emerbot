package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/emerson/emerbot/packages/domain"
)

type InMemoryStores struct {
	mu        sync.RWMutex
	shortTerm map[string][]domain.ConversationMessage
	longTerm  map[string][]domain.Memory
}

func NewInMemoryStores() *InMemoryStores {
	return &InMemoryStores{
		shortTerm: make(map[string][]domain.ConversationMessage),
		longTerm:  make(map[string][]domain.Memory),
	}
}

func (s *InMemoryStores) Append(_ context.Context, userID string, message domain.ConversationMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shortTerm[userID] = append(s.shortTerm[userID], message)
	return nil
}

func (s *InMemoryStores) LoadRecent(_ context.Context, userID string, limit int) ([]domain.ConversationMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := append([]domain.ConversationMessage(nil), s.shortTerm[userID]...)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (s *InMemoryStores) Save(_ context.Context, memory domain.Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.longTerm[memory.UserID]
	for idx := range items {
		if items[idx].Key() == memory.Key() {
			items[idx] = memory
			s.longTerm[memory.UserID] = items
			return nil
		}
	}

	s.longTerm[memory.UserID] = append(items, memory)
	sort.Slice(s.longTerm[memory.UserID], func(i, j int) bool {
		return s.longTerm[memory.UserID][i].Key() < s.longTerm[memory.UserID][j].Key()
	})
	return nil
}

func (s *InMemoryStores) LoadByUser(_ context.Context, userID string) ([]domain.Memory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return append([]domain.Memory(nil), s.longTerm[userID]...), nil
}
