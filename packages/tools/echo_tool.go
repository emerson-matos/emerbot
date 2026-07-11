package tools

import (
	"context"

	"github.com/emerson/emerbot/packages/domain"
)

type EchoTool struct{}

func (EchoTool) Name() string {
	return "echo"
}

func (EchoTool) Description() string {
	return "Retorna o texto recebido. Útil para validar o fluxo de tools."
}

func (EchoTool) Execute(_ context.Context, _ string, input string) (domain.ToolResult, error) {
	return domain.ToolResult{
		Name:   "echo",
		Output: input,
	}, nil
}
