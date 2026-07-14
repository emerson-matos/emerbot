package auth

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func TestInMemoryStoreUserLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewInMemoryStore()

	user := domain.User{UserID: "u1", Email: "alice@example.com", PasswordHash: "hash", Name: "Alice"}
	if err := s.SaveUser(ctx, user); err != nil {
		t.Fatalf("SaveUser returned error: %v", err)
	}

	byEmail, err := s.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail returned error: %v", err)
	}
	if byEmail.UserID != "u1" {
		t.Fatalf("expected user id u1, got %q", byEmail.UserID)
	}

	byID, err := s.GetUserByID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetUserByID returned error: %v", err)
	}
	if byID.Email != "alice@example.com" {
		t.Fatalf("expected email alice@example.com, got %q", byID.Email)
	}

	if _, err := s.GetUserByEmail(ctx, "missing@example.com"); err == nil {
		t.Fatal("expected error for unknown email")
	}
	if _, err := s.GetUserByID(ctx, "missing"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestInMemoryStoreRefreshTokenLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := NewInMemoryStore()

	if err := s.SaveRefreshToken(ctx, "u1", "tok", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SaveRefreshToken returned error: %v", err)
	}
	userID, err := s.ValidateRefreshToken(ctx, "tok")
	if err != nil {
		t.Fatalf("ValidateRefreshToken returned error: %v", err)
	}
	if userID != "u1" {
		t.Fatalf("expected user id u1, got %q", userID)
	}

	if err := s.RevokeRefreshToken(ctx, "tok"); err != nil {
		t.Fatalf("RevokeRefreshToken returned error: %v", err)
	}
	if _, err := s.ValidateRefreshToken(ctx, "tok"); err == nil {
		t.Fatal("expected error validating a revoked token")
	}

	if err := s.SaveRefreshToken(ctx, "u2", "expired", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("SaveRefreshToken returned error: %v", err)
	}
	if _, err := s.ValidateRefreshToken(ctx, "expired"); err == nil {
		t.Fatal("expected error validating an expired token")
	}
}
