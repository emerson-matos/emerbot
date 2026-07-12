package auth

import (
	"context"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// Store defines persistence operations for authentication.
type Store interface {
	GetUserByEmail(ctx context.Context, email string) (domain.User, error)
	GetUserByID(ctx context.Context, userID string) (domain.User, error)
	SaveUser(ctx context.Context, user domain.User) error

	SaveRefreshToken(ctx context.Context, userID, token string, expiresAt time.Time) error
	ValidateRefreshToken(ctx context.Context, token string) (userID string, err error)
	RevokeRefreshToken(ctx context.Context, token string) error
}
