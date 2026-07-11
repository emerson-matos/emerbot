package memory

import (
	"context"

	"github.com/emerson/emerbot/packages/domain"
)

type ShortTermStore interface {
	Append(ctx context.Context, userID string, message domain.ConversationMessage) error
	LoadRecent(ctx context.Context, userID string, limit int) ([]domain.ConversationMessage, error)
}

type LongTermStore interface {
	Save(ctx context.Context, memory domain.Memory) error
	LoadByUser(ctx context.Context, userID string) ([]domain.Memory, error)
}

