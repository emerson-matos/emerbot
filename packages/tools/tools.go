package tools

import (
	"context"

	"github.com/emerson/emerbot/packages/domain"
)

type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, userID string, input string) (domain.ToolResult, error)
}

