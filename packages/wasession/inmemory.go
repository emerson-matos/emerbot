package wasession

import (
	"context"
	"sync"
	"time"
)

// InMemoryStore implements Store for tests and local development.
type InMemoryStore struct {
	mu        sync.RWMutex
	expires   map[string]time.Time // key: phone -> session expiry
	processed map[string]time.Time // key: message ID -> dedup expiry
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		expires:   make(map[string]time.Time),
		processed: make(map[string]time.Time),
	}
}

func (s *InMemoryStore) RecordInbound(_ context.Context, phone string, at time.Time) error {
	exp := at.Add(Window)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Only ever extend the window — a delayed retry of an older message must
	// not shorten it.
	if cur, ok := s.expires[phone]; !ok || exp.After(cur) {
		s.expires[phone] = exp
	}
	return nil
}

func (s *InMemoryStore) MarkProcessed(_ context.Context, messageID string, now time.Time) (bool, error) {
	if messageID == "" {
		return true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if exp, ok := s.processed[messageID]; ok && exp.After(now) {
		return false, nil
	}
	s.processed[messageID] = now.Add(DedupWindow)
	return true, nil
}

func (s *InMemoryStore) Active(_ context.Context, phone string, now time.Time) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.expires[phone]
	return ok && exp.After(now), nil
}
