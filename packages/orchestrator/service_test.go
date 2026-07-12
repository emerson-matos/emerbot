package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/tools"
)

func TestHandleMessageRejectsInvalidMessage(t *testing.T) {
	t.Parallel()

	service := newTestService(stubLLM{output: llm.Output{Text: "ok"}}, tools.NewRegistry(tools.EchoTool{}))
	_, err := service.HandleMessage(context.Background(), domain.Message{})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestHandleMessageUsesDefaultResponseWhenLLMReturnsBlank(t *testing.T) {
	t.Parallel()

	service := newTestService(stubLLM{output: llm.Output{Text: "   "}}, tools.NewRegistry(tools.EchoTool{}))
	response, err := service.HandleMessage(context.Background(), validMessage("oi"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if response.Text != "Não consegui gerar uma resposta." {
		t.Fatalf("unexpected fallback response: %q", response.Text)
	}
}

func TestHandleMessageExecutesToolCall(t *testing.T) {
	t.Parallel()

	service := newTestService(stubLLM{output: llm.Output{
		Text: "Vou usar uma tool.",
		ToolCall: &domain.ToolCall{
			Name:  " echo ",
			Input: "payload",
		},
	}}, tools.NewRegistry(tools.EchoTool{}))

	response, err := service.HandleMessage(context.Background(), validMessage("tool"))
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if len(response.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(response.ToolResults))
	}
	if response.ToolResults[0].Output != "payload" {
		t.Fatalf("unexpected tool output: %q", response.ToolResults[0].Output)
	}
}

func TestHandleMessageReturnsToolExecutionError(t *testing.T) {
	t.Parallel()

	service := newTestService(stubLLM{output: llm.Output{
		Text: "Vou usar uma tool.",
		ToolCall: &domain.ToolCall{
			Name: "missing",
		},
	}}, tools.NewRegistry(tools.EchoTool{}))

	_, err := service.HandleMessage(context.Background(), validMessage("tool"))
	if err == nil {
		t.Fatal("expected tool execution error")
	}
}

type stubLLM struct {
	output llm.Output
	err    error
}

func (s stubLLM) Generate(_ context.Context, _ llm.Input) (llm.Output, error) {
	if s.err != nil {
		return llm.Output{}, s.err
	}
	return s.output, nil
}

func newTestService(client llm.Client, registry *tools.Registry) *Service {
	stores := memory.NewInMemoryStores()
	if err := stores.Save(context.Background(), domain.Memory{
		UserID: "u1",
		Type:   "Preference",
		ID:     "Tone",
		Value:  "Concise",
	}); err != nil {
		panic(err)
	}

	return NewService(client, stores, stores, registry)
}

func validMessage(text string) domain.Message {
	return domain.Message{
		UserID:    "u1",
		Text:      text,
		Timestamp: time.Now().UTC(),
		MessageID: "m1",
	}
}
