package llm

import (
	"context"

	"github.com/emerson/emerbot/packages/domain"
)

type Input struct {
	UserMessage  domain.Message
	ShortTerm    []domain.ConversationMessage
	LongTerm     []domain.Memory
	Available    []ToolDefinition
	SystemPrompt string
}

type ToolDefinition struct {
	Name        string
	Description string
}

type Output struct {
	Text     string
	ToolCall *domain.ToolCall
}

type Client interface {
	Generate(ctx context.Context, input Input) (Output, error)
}

