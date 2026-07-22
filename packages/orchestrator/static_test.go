package orchestrator

import (
	"context"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

func TestStaticClientToolPrefixRequiresPrefixAndPreservesName(t *testing.T) {
	t.Parallel()

	client := StaticClient{}
	output, err := client.Generate(context.Background(), Input{
		UserMessage: domain.Message{
			UserID:    "u1",
			Text:      "Tool: echo",
			MessageID: "m1",
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if output.ToolCall == nil {
		t.Fatal("expected tool call")
	}
	if output.ToolCall.Name != "echo" {
		t.Fatalf("expected tool name echo, got %q", output.ToolCall.Name)
	}
}

func TestStaticClientIgnoresEmbeddedToolMarker(t *testing.T) {
	t.Parallel()

	client := StaticClient{}
	output, err := client.Generate(context.Background(), Input{
		UserMessage: domain.Message{
			UserID:    "u1",
			Text:      "quero usar tool: echo",
			MessageID: "m1",
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if output.ToolCall != nil {
		t.Fatal("expected no tool call for embedded tool marker")
	}
}
