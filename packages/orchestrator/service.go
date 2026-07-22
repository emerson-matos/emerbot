package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator/internal/gemini"
	"github.com/emerson/emerbot/packages/shared"
)

type Config struct {
	GeminiAPIKey string
	FinanceStore finance.Store
}

type Service struct {
	generator        TextGenerator
	shortTerm        ShortTermStore
	longTerm         LongTermStore
	tools            *Registry
	shortTermLimit   int
	defaultResponder string
}

func NewService(cfg Config) *Service {
	stores := NewInMemoryStores()
	var gen TextGenerator = StaticClient{}
	if cfg.GeminiAPIKey != "" && cfg.FinanceStore != nil {
		agent, err := gemini.NewAgent(context.Background(), cfg.GeminiAPIKey, cfg.FinanceStore)
		if err != nil {
			log.Printf("orchestrator: gemini agent: %v, using static fallback", err)
		} else {
			gen = &geminiGenerator{agent: agent}
		}
	}
	return &Service{
		generator:        gen,
		shortTerm:        stores,
		longTerm:         stores,
		tools:            NewRegistry(EchoTool{}),
		shortTermLimit:   10,
		defaultResponder: "Não consegui gerar uma resposta.",
	}
}

// financeAgent is the slice of *gemini.Agent that geminiGenerator needs; it
// lets tests inject a fake without a real Gemini client.
type financeAgent interface {
	Process(ctx context.Context, userID, text string, msgTime time.Time) (string, error)
}

type geminiGenerator struct {
	agent financeAgent
}

// Generate always processes against the shared finance ledger, not the
// sender's own user ID — the agent reads/writes finance entries, and every
// sender must land in the same ledger slash commands use (see
// shared.FinanceLedgerID) until real phone→account linking exists.
func (g *geminiGenerator) Generate(ctx context.Context, input Input) (Output, error) {
	reply, err := g.agent.Process(ctx, shared.FinanceLedgerID, input.UserMessage.Text, input.UserMessage.Timestamp)
	if err != nil {
		return Output{}, fmt.Errorf("gemini: %w", err)
	}
	return Output{Text: reply}, nil
}

func NewServiceWithGenerator(gen TextGenerator) *Service {
	stores := NewInMemoryStores()
	return &Service{
		generator:        gen,
		shortTerm:        stores,
		longTerm:         stores,
		tools:            NewRegistry(EchoTool{}),
		shortTermLimit:   10,
		defaultResponder: "Não consegui gerar uma resposta.",
	}
}

func (s *Service) HandleMessage(ctx context.Context, message domain.Message) (domain.Response, error) {
	if err := s.validateMessage(message); err != nil {
		return domain.Response{}, err
	}

	if err := s.shortTerm.Append(ctx, message.UserID, domain.ConversationMessage{
		Role:      domain.RoleUser,
		Text:      message.Text,
		Timestamp: message.Timestamp,
	}); err != nil {
		return domain.Response{}, fmt.Errorf("append user message: %w", err)
	}

	shortTerm, err := s.shortTerm.LoadRecent(ctx, message.UserID, s.shortTermLimit)
	if err != nil {
		return domain.Response{}, fmt.Errorf("load short term memory: %w", err)
	}

	longTerm, err := s.longTerm.LoadByUser(ctx, message.UserID)
	if err != nil {
		return domain.Response{}, fmt.Errorf("load long term memory: %w", err)
	}

	output, err := s.generator.Generate(ctx, Input{
		UserMessage:  message,
		ShortTerm:    shortTerm,
		LongTerm:     longTerm,
		SystemPrompt: systemPrompt(),
	})
	if err != nil {
		return domain.Response{}, fmt.Errorf("generate llm response: %w", err)
	}

	response := domain.Response{
		Text:    strings.TrimSpace(output.Text),
		UsedLLM: true,
	}
	if response.Text == "" {
		response.Text = s.defaultResponder
	}

	if output.ToolCall != nil {
		result, execErr := s.tools.Execute(ctx, *output.ToolCall, message.UserID)
		if execErr != nil {
			return domain.Response{}, fmt.Errorf("execute tool: %w", execErr)
		}
		response.ToolResults = append(response.ToolResults, result)
		response.Text = response.Text + "\n\nTool " + result.Name + ": " + result.Output
	}

	if err := s.shortTerm.Append(ctx, message.UserID, domain.ConversationMessage{
		Role:      domain.RoleAssistant,
		Text:      response.Text,
		Timestamp: time.Now().UTC(),
	}); err != nil {
		return domain.Response{}, fmt.Errorf("append assistant message: %w", err)
	}

	return response, nil
}

func (s *Service) validateMessage(message domain.Message) error {
	if strings.TrimSpace(message.UserID) == "" {
		return fmt.Errorf("user id is required")
	}
	if strings.TrimSpace(message.MessageID) == "" {
		return fmt.Errorf("message id is required")
	}
	if message.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}
	return nil
}

func systemPrompt() string {
	return "Você é um assistente pessoal via WhatsApp. Seja objetivo, mantenha contexto e use tools apenas quando necessário."
}
