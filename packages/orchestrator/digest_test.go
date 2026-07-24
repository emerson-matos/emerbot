package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

type fakeWriter struct {
	reply      string
	err        error
	gotSystem  string
	gotMessage string
	called     bool
}

func (f *fakeWriter) Generate(_ context.Context, systemPrompt, message string) (string, error) {
	f.called = true
	f.gotSystem = systemPrompt
	f.gotMessage = message
	if f.err != nil {
		return "", f.err
	}
	return f.reply, nil
}

// TestDirectGeneratorForwardsSystemPromptAndMessage is the regression test for
// the notifier bug: the digest sends its draft as UserMessage.Text plus a
// SystemPrompt and an empty history. The agent-based generator dropped both and
// errored on the empty history; directGenerator must forward exactly those two
// fields to the writer.
func TestDirectGeneratorForwardsSystemPromptAndMessage(t *testing.T) {
	t.Parallel()

	w := &fakeWriter{reply: "texto humanizado"}
	gen := &directGenerator{writer: w}

	out, err := gen.Generate(context.Background(), Input{
		UserMessage:  domain.Message{Text: "rascunho do resumo"},
		SystemPrompt: "seja amigável",
		// No ShortTerm on purpose — the digest never populates it.
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Text != "texto humanizado" {
		t.Fatalf("unexpected output: %q", out.Text)
	}
	if !w.called {
		t.Fatal("expected the writer to be called")
	}
	if w.gotSystem != "seja amigável" {
		t.Fatalf("system prompt not forwarded: %q", w.gotSystem)
	}
	if w.gotMessage != "rascunho do resumo" {
		t.Fatalf("message not forwarded: %q", w.gotMessage)
	}
}

func TestDirectGeneratorPropagatesWriterError(t *testing.T) {
	t.Parallel()

	gen := &directGenerator{writer: &fakeWriter{err: errors.New("boom")}}
	if _, err := gen.Generate(context.Background(), Input{
		UserMessage: domain.Message{Text: "x"},
	}); err == nil {
		t.Fatal("expected the writer error to propagate")
	}
}

// TestNewDigestGeneratorFallsBackToEcho proves that without a Gemini key the
// factory degrades to the echo generator (not a nil generator, and not
// StaticClient, whose "Resposta local do orchestrator:" prefix would leak into
// the digest). The echo generator returns the caller's draft verbatim so the
// notifier's static template is delivered unchanged.
func TestNewDigestGeneratorFallsBackToEcho(t *testing.T) {
	t.Parallel()

	gen := NewDigestGenerator(Config{})
	if _, ok := gen.(echoGenerator); !ok {
		t.Fatalf("expected echoGenerator without a Gemini key, got %T", gen)
	}

	draft := "🔔 *Farmácia Financeira* — resumo de hoje:\n• conta vence hoje"
	out, err := gen.Generate(context.Background(), Input{
		UserMessage:  domain.Message{Text: draft},
		SystemPrompt: "seja amigável",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out.Text != draft {
		t.Fatalf("echo must return the draft verbatim: got %q, want %q", out.Text, draft)
	}
}

// TestNewDigestGeneratorBuildsDirectGenerator proves the Gemini key selects the
// tool-less direct path (not the agent-based generator that caused the bug).
func TestNewDigestGeneratorBuildsDirectGenerator(t *testing.T) {
	t.Parallel()

	gen := NewDigestGenerator(Config{GeminiAPIKey: "test-key"})
	if _, ok := gen.(*directGenerator); !ok {
		t.Fatalf("expected *directGenerator with a Gemini key, got %T", gen)
	}
}
