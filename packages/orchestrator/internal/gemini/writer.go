package gemini

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// Writer is a one-shot text generator: it makes a single GenerateContent call
// with a system prompt plus one user message and returns the text. Unlike Agent
// it exposes no tools and runs no multi-round loop — the right shape for copy
// like the daily digest, where the model rewrites a draft and never needs to
// fetch data.
type Writer struct {
	gen   contentGenerator
	model string
}

// NewWriter builds a Writer backed by the Gemini API. It shares Agent's model
// but carries none of its finance tools.
func NewWriter(ctx context.Context, apiKey string) (*Writer, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}
	return &Writer{gen: client.Models, model: model}, nil
}

// Generate sends systemPrompt as the system instruction and message as the sole
// user turn, returning the model's text. An empty message is rejected up front,
// and an empty or textless response is reported as an error so the caller can
// fall back rather than send a blank message.
func (w *Writer) Generate(ctx context.Context, systemPrompt, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("gemini writer: empty message")
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	config := &genai.GenerateContentConfig{
		Temperature:     genai.Ptr[float32](0),
		MaxOutputTokens: 1024,
	}
	if strings.TrimSpace(systemPrompt) != "" {
		config.SystemInstruction = &genai.Content{Parts: []*genai.Part{{Text: systemPrompt}}}
	}

	contents := []*genai.Content{{Role: "user", Parts: []*genai.Part{{Text: message}}}}
	resp, err := w.gen.GenerateContent(ctx, w.model, contents, config)
	if err != nil {
		return "", fmt.Errorf("gemini writer generate: %w", err)
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("gemini writer: empty response")
	}
	reply := candidateText(resp.Candidates[0].Content)
	if reply == "" {
		return "", fmt.Errorf("gemini writer: no text in response")
	}
	return reply, nil
}
