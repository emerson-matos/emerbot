package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// InMemoryStore implements Store for tests.
type InMemoryStore struct {
	mu            sync.RWMutex
	usersByID     map[string]domain.User
	usersByEmail  map[string]domain.User
	refreshTokens map[string]refreshTokenRecord
}

type refreshTokenRecord struct {
	userID    string
	expiresAt time.Time
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		usersByID:     make(map[string]domain.User),
		usersByEmail:  make(map[string]domain.User),
		refreshTokens: make(map[string]refreshTokenRecord),
	}
}

func (s *InMemoryStore) SaveUser(_ context.Context, user domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usersByID[user.UserID] = user
	s.usersByEmail[user.Email] = user
	return nil
}

func (s *InMemoryStore) GetUserByEmail(_ context.Context, email string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.usersByEmail[email]
	if !ok {
		return domain.User{}, fmt.Errorf("user with email %q not found", email)
	}
	return u, nil
}

func (s *InMemoryStore) GetUserByID(_ context.Context, userID string) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.usersByID[userID]
	if !ok {
		return domain.User{}, fmt.Errorf("user %q not found", userID)
	}
	return u, nil
}

func (s *InMemoryStore) SaveRefreshToken(_ context.Context, userID, token string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshTokens[token] = refreshTokenRecord{userID: userID, expiresAt: expiresAt}
	return nil
}

func (s *InMemoryStore) ValidateRefreshToken(_ context.Context, token string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.refreshTokens[token]
	if !ok {
		return "", fmt.Errorf("refresh token not found")
	}
	if time.Now().After(rec.expiresAt) {
		return "", fmt.Errorf("refresh token expired")
	}
	return rec.userID, nil
}

func (s *InMemoryStore) RevokeRefreshToken(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.refreshTokens, token)
	return nil
}
