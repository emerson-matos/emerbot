package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/emerson/emerbot/packages/domain"
)

// StaticClient is a deterministic adapter for local development.
type StaticClient struct{}

func (c StaticClient) Generate(_ context.Context, input Input) (Output, error) {
	text := strings.TrimSpace(input.UserMessage.Text)
	if text == "" {
		return Output{Text: "Não recebi nenhuma mensagem."}, nil
	}

	lower := strings.ToLower(text)
	if (strings.Contains(lower, "memória") || strings.Contains(lower, "memoria")) && len(input.LongTerm) > 0 {
		return Output{Text: "Encontrei informações salvas sobre você e considerei isso na resposta."}, nil
	}

	if strings.HasPrefix(lower, "tool:") {
		name := strings.TrimSpace(text[len("tool:"):])
		if name == "" {
			return Output{}, fmt.Errorf("tool name is required after tool: prefix")
		}

		return Output{
			Text: "Vou consultar uma tool antes de responder.",
			ToolCall: &domain.ToolCall{
				Name:   name,
				Input:  input.UserMessage.Text,
				Reason: "Solicitação explícita em ambiente local.",
			},
		}, nil
	}

	return Output{Text: "Resposta local do orchestrator: " + text}, nil
}
