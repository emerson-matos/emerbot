// Package ollama is a dev-only LLM provider that runs the finance agent against
// a locally served open-source model (Llama, Qwen, …) through Ollama's
// /api/chat endpoint. It mirrors the Gemini agent's tool-calling loop so the
// same finance tools work offline, and talks plain HTTP (no extra dependency).
// Production still uses Gemini — see ADR-012.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator/internal/agentprompt"
)

const (
	DefaultHost  = "http://localhost:11434"
	DefaultModel = "llama3.1:8b"

	// timeout is generous because local CPU inference is slow and a turn may span
	// several tool rounds; the local webhook runs as a plain HTTP server without
	// the Lambda 10s cap.
	timeout       = 120 * time.Second
	maxToolRounds = 5
)

// Agent talks to Ollama's /api/chat with the finance tools attached.
type Agent struct {
	httpClient   *http.Client
	host         string
	model        string
	tools        []tool
	toolHandlers map[string]finance.ToolFunc
}

// NewAgent builds an Ollama-backed finance agent. Empty host/model fall back to
// the local defaults. It never dials Ollama here — the connection is lazy, made
// on the first Process call.
func NewAgent(host, model string, store finance.Store) *Agent {
	if host == "" {
		host = DefaultHost
	}
	if model == "" {
		model = DefaultModel
	}

	financeTools := finance.FinanceTools(store)
	tools := make([]tool, len(financeTools))
	handlers := make(map[string]finance.ToolFunc, len(financeTools))
	for i, t := range financeTools {
		tools[i] = tool{
			Type: "function",
			Function: toolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  schemaToJSON(t.Parameters),
			},
		}
		handlers[t.Name] = t.Handler
	}
	return &Agent{
		httpClient:   &http.Client{},
		host:         strings.TrimRight(host, "/"),
		model:        model,
		tools:        tools,
		toolHandlers: handlers,
	}
}

// Process runs the tool-calling loop over the recent conversation `history`
// (oldest-first, ending with the current user turn). It matches the Gemini
// agent's signature so both satisfy the orchestrator's financeAgent interface.
func (a *Agent) Process(ctx context.Context, userID string, history []domain.ConversationMessage, msgTime time.Time) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	messages := make([]message, 0, len(history)+1)
	messages = append(messages, message{Role: "system", Content: agentprompt.Finance(msgTime)})
	messages = append(messages, historyToMessages(history)...)
	if len(messages) == 1 {
		return "", fmt.Errorf("ollama agent: empty conversation history")
	}

	for round := range maxToolRounds {
		resp, err := a.chat(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("ollama chat (round %d): %w", round, err)
		}

		reply := resp.Message
		if len(reply.ToolCalls) == 0 {
			if text := strings.TrimSpace(reply.Content); text != "" {
				return text, nil
			}
			return "", fmt.Errorf("ollama returned neither text nor tool call (round %d)", round)
		}

		// Echo the assistant's tool-call turn, then answer each call.
		messages = append(messages, reply)
		for _, tc := range reply.ToolCalls {
			result, err := a.runTool(ctx, userID, tc)
			payload := map[string]any{"output": result}
			if err != nil {
				log.Printf("ollama agent tool %s error: %v", tc.Function.Name, err)
				payload = map[string]any{"error": err.Error()}
			}
			body, _ := json.Marshal(payload)
			messages = append(messages, message{
				Role:     "tool",
				Content:  string(body),
				ToolName: tc.Function.Name,
			})
		}
	}

	return "", fmt.Errorf("ollama agent: exceeded %d tool rounds", maxToolRounds)
}

func (a *Agent) runTool(ctx context.Context, userID string, tc toolCall) (any, error) {
	handler, ok := a.toolHandlers[tc.Function.Name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", tc.Function.Name)
	}
	argsJSON, err := json.Marshal(tc.Function.Arguments)
	if err != nil {
		return nil, fmt.Errorf("marshal args for %s: %w", tc.Function.Name, err)
	}
	result, err := handler(ctx, userID, argsJSON)
	if err != nil {
		return nil, fmt.Errorf("tool %s: %w", tc.Function.Name, err)
	}
	return result, nil
}

func (a *Agent) chat(ctx context.Context, messages []message) (*chatResponse, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model:    a.model,
		Messages: messages,
		Tools:    a.tools,
		Stream:   false,
		Options:  options{Temperature: 0},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.host+"/api/chat", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := a.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call ollama: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d", httpResp.StatusCode)
	}

	var resp chatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

// historyToMessages maps stored turns onto Ollama chat messages, translating
// roles (Ollama uses "assistant") and dropping turns it can't represent.
func historyToMessages(history []domain.ConversationMessage) []message {
	out := make([]message, 0, len(history))
	for _, m := range history {
		role := ollamaRole(m.Role)
		if role == "" || strings.TrimSpace(m.Text) == "" {
			continue
		}
		out = append(out, message{Role: role, Content: m.Text})
	}
	return out
}

func ollamaRole(role domain.Role) string {
	switch role {
	case domain.RoleUser:
		return "user"
	case domain.RoleAssistant:
		return "assistant"
	default:
		return ""
	}
}

// schemaToJSON converts a genai.Schema (how finance tools declare parameters)
// into the plain JSON Schema object Ollama expects, lowercasing the type names.
func schemaToJSON(s *genai.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object"}
	}
	out := map[string]any{}
	if s.Type != "" && s.Type != genai.TypeUnspecified {
		out["type"] = strings.ToLower(string(s.Type))
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = schemaToJSON(v)
		}
		out["properties"] = props
	}
	if s.Items != nil {
		out["items"] = schemaToJSON(s.Items)
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	return out
}
