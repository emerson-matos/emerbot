package gemini

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/genai"
)

func newTestWriter(gen contentGenerator) *Writer {
	return &Writer{gen: gen, model: model}
}

func TestWriterReturnsTextInSingleCall(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		textResponse("Bom dia! Você tem uma conta vencendo hoje."),
	}}
	w := newTestWriter(gen)

	reply, err := w.Generate(context.Background(), "seja amigável", "🔔 resumo: conta vence hoje")
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if reply != "Bom dia! Você tem uma conta vencendo hoje." {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if gen.calls != 1 {
		t.Fatalf("expected a single Gemini call, got %d", gen.calls)
	}
}

func TestWriterExposesNoTools(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{textResponse("ok")}}
	w := newTestWriter(gen)

	if _, err := w.Generate(context.Background(), "sys", "msg"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gen.lastTools != nil {
		t.Fatalf("writer must not expose tools, got %v", gen.lastTools)
	}
}

func TestWriterSendsSystemPromptAndMessage(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{textResponse("ok")}}
	w := newTestWriter(gen)

	if _, err := w.Generate(context.Background(), "instrução do sistema", "rascunho do resumo"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// The one and only turn sent to Gemini must be the message, as the user role.
	if len(gen.firstContents) != 1 {
		t.Fatalf("expected exactly 1 content turn, got %d", len(gen.firstContents))
	}
	if gen.firstContents[0].Role != "user" {
		t.Fatalf("expected the message to be a user turn, got role %q", gen.firstContents[0].Role)
	}
	if got := gen.firstContents[0].Parts[0].Text; got != "rascunho do resumo" {
		t.Fatalf("message not sent through: %q", got)
	}
}

func TestWriterRejectsEmptyMessageWithoutCallingGemini(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{textResponse("ok")}}
	w := newTestWriter(gen)

	if _, err := w.Generate(context.Background(), "sys", "   "); err == nil {
		t.Fatal("expected an error for an empty message")
	}
	if gen.calls != 0 {
		t.Fatalf("empty message must not reach Gemini, calls=%d", gen.calls)
	}
}

func TestWriterPropagatesGeneratorError(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{err: errors.New("network down")}
	w := newTestWriter(gen)

	if _, err := w.Generate(context.Background(), "sys", "msg"); err == nil {
		t.Fatal("expected the generator error to propagate")
	}
}

func TestWriterErrorsOnTextlessResponse(t *testing.T) {
	t.Parallel()

	gen := &scriptedGenerator{responses: []*genai.GenerateContentResponse{
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Role: "model", Parts: []*genai.Part{}}}}},
	}}
	w := newTestWriter(gen)

	if _, err := w.Generate(context.Background(), "sys", "msg"); err == nil {
		t.Fatal("expected an error when the response carries no text")
	}
}
