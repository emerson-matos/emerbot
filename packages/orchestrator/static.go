package orchestrator

import (
	"context"
	"strings"
)

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

	return Output{Text: "Resposta local do orchestrator: " + text}, nil
}
