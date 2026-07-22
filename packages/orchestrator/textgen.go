package orchestrator

import (
	"context"

	"github.com/emerson/emerbot/packages/domain"
)

type Input struct {
	UserMessage  domain.Message
	ShortTerm    []domain.ConversationMessage
	LongTerm     []domain.Memory
	SystemPrompt string
}

type Output struct {
	Text     string
	ToolCall *domain.ToolCall
}

type TextGenerator interface {
	Generate(ctx context.Context, input Input) (Output, error)
}
