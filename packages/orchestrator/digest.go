package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/emerson/emerbot/packages/orchestrator/internal/gemini"
)

// textWriter is a one-shot generator: system prompt + message in, text out, with
// no tools and no conversation history. gemini.Writer satisfies it; tests inject
// a fake.
type textWriter interface {
	Generate(ctx context.Context, systemPrompt, message string) (string, error)
}

// directGenerator adapts a textWriter to the TextGenerator interface for callers
// that supply a system prompt and a single message (e.g. the notifier's daily
// digest) and neither have a conversation history nor need the agent's tool loop.
// It reads Input.SystemPrompt and Input.UserMessage.Text directly — the exact
// fields the agent-based generator ignores.
type directGenerator struct {
	writer textWriter
}

func (g *directGenerator) Generate(ctx context.Context, input Input) (Output, error) {
	reply, err := g.writer.Generate(ctx, input.SystemPrompt, input.UserMessage.Text)
	if err != nil {
		return Output{}, fmt.Errorf("llm writer: %w", err)
	}
	return Output{Text: reply}, nil
}

// echoGenerator returns the caller's message unchanged — the digest's no-LLM
// fallback. Its callers (the notifier) already pass a ready-to-send draft as
// Input.UserMessage.Text, so echoing it back delivers that draft verbatim. This
// is deliberately not StaticClient, whose chat-oriented Generate wraps the text
// in a "Resposta local do orchestrator:" prefix that would leak into the digest.
type echoGenerator struct{}

func (echoGenerator) Generate(_ context.Context, input Input) (Output, error) {
	return Output{Text: input.UserMessage.Text}, nil
}

// NewDigestGenerator builds a tool-less text generator for one-shot copy such as
// the daily notifier digest, where the model rewrites a draft into friendlier
// prose and never needs finance tools. Unlike NewTextGenerator, the returned
// generator honors Input.SystemPrompt and Input.UserMessage.Text instead of
// replaying a conversation history — so a caller that sends an empty history
// (the digest does) actually reaches the model instead of erroring out.
//
// When Gemini isn't configured it falls back to echoGenerator, which returns the
// caller's own draft unchanged. The digest is thus delivered as-is rather than
// wrapped or rewritten — the tool-less counterpart to how the digest already
// falls back to its static template on any generation error. Only the Gemini
// path is wired here; the notifier never selects the ollama provider.
func NewDigestGenerator(cfg Config) TextGenerator {
	if cfg.GeminiAPIKey != "" {
		writer, err := gemini.NewWriter(context.Background(), cfg.GeminiAPIKey)
		if err != nil {
			log.Printf("orchestrator: gemini writer: %v, using echo fallback", err)
		} else {
			return &directGenerator{writer: writer}
		}
	}
	return echoGenerator{}
}
