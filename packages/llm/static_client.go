package llm

import (
	"context"
	"strings"

	"github.com/emerson/emerbot/packages/domain"
)

// StaticClient is a deterministic adapter for local development.
type StaticClient struct{}

func (c StaticClient) Generate(_ context.Context, input Input) (Output, error) {
	text := strings.TrimSpace(input.UserMessage.Text)
	if text == "" {
		return Output{Text: "Nao recebi nenhuma mensagem."}, nil
	}

	lower := strings.ToLower(text)
	if strings.Contains(lower, "memoria") && len(input.LongTerm) > 0 {
		return Output{Text: "Encontrei informacoes salvas sobre voce e considerei isso na resposta."}, nil
	}

	if strings.Contains(lower, "tool:") {
		name := strings.TrimSpace(strings.TrimPrefix(text, "tool:"))
		return Output{
			Text: "Vou consultar uma tool antes de responder.",
			ToolCall: &domain.ToolCall{
				Name:   name,
				Input:  input.UserMessage.Text,
				Reason: "Solicitacao explicita em ambiente local.",
			},
		}, nil
	}

	return Output{Text: "Resposta local do orchestrator: " + text}, nil
}

