package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/finance"
)

// geminiModel is the model both the agent and any regex fallback report; kept as
// a single source of truth for the deployed Gemini model name.
const geminiModel = "gemini-3.1-flash-lite"

// agentTimeout bounds a whole agent turn (which may chain several Gemini calls
// plus DynamoDB tool executions), so a slow API never stalls the webhook.
const agentTimeout = 30 * time.Second

// maxToolRounds caps how many function-calling round-trips a single message may
// trigger, so a model that keeps calling tools can't loop forever.
const maxToolRounds = 5

// contentGenerator is the slice of *genai.Models the agent needs; it lets tests
// inject a fake without network access.
type contentGenerator interface {
	GenerateContent(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

// GeminiAgent answers free-text financial messages using Gemini function
// calling: the model decides which finance tool to invoke, the agent runs it
// against the store, feeds the result back, and repeats until the model returns
// a plain-text reply for the user.
type GeminiAgent struct {
	gen          contentGenerator
	model        string
	tools        []*genai.Tool
	toolHandlers map[string]finance.ToolHandler
}

// NewGeminiAgent builds an agent backed by the real Gemini API and the given
// finance store (used by the tools to read/write entries).
func NewGeminiAgent(ctx context.Context, apiKey string, store finance.Store) (*GeminiAgent, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}

	tools, handlers := finance.FinanceTools(store)
	return &GeminiAgent{
		gen:          client.Models,
		model:        geminiModel,
		tools:        tools,
		toolHandlers: handlers,
	}, nil
}

func agentSystemPrompt(now time.Time) string {
	return fmt.Sprintf(
		`Você é um assistente financeiro de uma farmácia.
Sua função é ajudar o usuário a gerenciar o fluxo de caixa.

Contexto atual:
- Hoje é %s
- Fuso horário: America/Sao_Paulo

Interprete datas relativas ("amanhã", "último dia do mês", "mês que vem")
usando a data acima como referência. Nunca invente datas.

Você tem acesso a ferramentas para criar lançamentos, consultar o resumo do
mês, listar contas a pagar/receber e buscar lançamentos.

Regras:
- Sempre use as ferramentas quando precisar de dados. Nunca invente valores.
- Responda em português, de forma clara e direta.
- Valores em reais (R$).
- Se a mensagem não for financeira, responda educadamente que você é um
  assistente financeiro e pode ajudar com o fluxo de caixa.`,
		now.Format("02/01/2006"),
	)
}

// Process handles a single free-text message and returns the reply to send.
func (a *GeminiAgent) Process(ctx context.Context, userID, text string, msgTime time.Time) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, agentTimeout)
	defer cancel()

	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: agentSystemPrompt(msgTime)}}},
		Tools:             a.tools,
		Temperature:       genai.Ptr[float32](0),
		MaxOutputTokens:   1024,
	}

	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: text}}},
	}

	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.gen.GenerateContent(ctx, a.model, contents, config)
		if err != nil {
			return "", fmt.Errorf("gemini generate (round %d): %w", round, err)
		}
		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
			return "", fmt.Errorf("gemini returned empty response (round %d)", round)
		}

		candidate := resp.Candidates[0].Content
		calls := functionCalls(candidate)

		// No tool calls → the model produced its final answer.
		if len(calls) == 0 {
			if reply := candidateText(candidate); reply != "" {
				return reply, nil
			}
			return "", fmt.Errorf("gemini returned neither text nor function call (round %d)", round)
		}

		// Execute every requested tool and feed the results back in one turn.
		responseParts := make([]*genai.Part, 0, len(calls))
		for _, fc := range calls {
			result, err := a.runTool(ctx, userID, fc)
			if err != nil {
				return "", err
			}
			responseParts = append(responseParts, &genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     fc.Name,
					Response: map[string]any{"output": result},
				},
			})
		}

		contents = append(
			contents,
			candidate,
			&genai.Content{Role: "user", Parts: responseParts},
		)
	}

	return "", fmt.Errorf("gemini agent: exceeded %d tool rounds", maxToolRounds)
}

func (a *GeminiAgent) runTool(ctx context.Context, userID string, fc *genai.FunctionCall) (any, error) {
	handler, ok := a.toolHandlers[fc.Name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", fc.Name)
	}
	argsJSON, err := json.Marshal(fc.Args)
	if err != nil {
		return nil, fmt.Errorf("marshal args for %s: %w", fc.Name, err)
	}
	result, err := handler(ctx, userID, argsJSON)
	if err != nil {
		return nil, fmt.Errorf("tool %s: %w", fc.Name, err)
	}
	return result, nil
}

func functionCalls(c *genai.Content) []*genai.FunctionCall {
	var calls []*genai.FunctionCall
	for _, p := range c.Parts {
		if p != nil && p.FunctionCall != nil {
			calls = append(calls, p.FunctionCall)
		}
	}
	return calls
}

func candidateText(c *genai.Content) string {
	var b strings.Builder
	for _, p := range c.Parts {
		if p != nil && p.Text != "" {
			b.WriteString(p.Text)
		}
	}
	return strings.TrimSpace(b.String())
}
