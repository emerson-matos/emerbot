package orchestrator

import (
	"context"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

func TestStaticClientReturnsLocalPrefix(t *testing.T) {
	t.Parallel()

	client := StaticClient{}
	output, err := client.Generate(context.Background(), Input{
		UserMessage: domain.Message{
			UserID:    "u1",
			Text:      "hello",
			MessageID: "m1",
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if output.Text != "Resposta local do orchestrator: hello" {
		t.Fatalf("unexpected text: %q", output.Text)
	}
}

func TestStaticClientReturnsEmptyTextWarning(t *testing.T) {
	t.Parallel()

	client := StaticClient{}
	output, err := client.Generate(context.Background(), Input{
		UserMessage: domain.Message{
			UserID:    "u1",
			Text:      "   ",
			MessageID: "m1",
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if output.Text != "Não recebi nenhuma mensagem." {
		t.Fatalf("unexpected text: %q", output.Text)
	}
}
