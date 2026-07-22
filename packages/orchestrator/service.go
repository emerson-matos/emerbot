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
	// ShortTerm persists conversation history. When nil, an in-memory store is
	// used (fine for local/dev, but lost on every Lambda cold start).
	ShortTerm ShortTermStore
}

type Service struct {
	generator        TextGenerator
	shortTerm        ShortTermStore
	longTerm         LongTermStore
	shortTermLimit   int
	defaultResponder string
}

func NewService(cfg Config) *Service {
	gen := NewTextGenerator(cfg)
	stores := NewInMemoryStores()
	var shortTerm ShortTermStore = stores
	if cfg.ShortTerm != nil {
		shortTerm = cfg.ShortTerm
	}
	return &Service{
		generator:        gen,
		shortTerm:        shortTerm,
		longTerm:         stores,
		shortTermLimit:   10,
		defaultResponder: "Não consegui gerar uma resposta.",
	}
}

func NewTextGenerator(cfg Config) TextGenerator {
	if cfg.GeminiAPIKey != "" && cfg.FinanceStore != nil {
		agent, err := gemini.NewAgent(context.Background(), cfg.GeminiAPIKey, cfg.FinanceStore)
		if err != nil {
			log.Printf("orchestrator: gemini agent: %v, using static fallback", err)
		} else {
			return &geminiGenerator{agent: agent}
		}
	}
	return StaticClient{}
}

// financeAgent lets tests inject a fake without a real Gemini client.
type financeAgent interface {
	Process(ctx context.Context, userID string, history []domain.ConversationMessage, msgTime time.Time) (string, error)
}

type geminiGenerator struct {
	agent financeAgent
}

func (g *geminiGenerator) Generate(ctx context.Context, input Input) (Output, error) {
	// input.ShortTerm already ends with the current user turn (HandleMessage
	// appends it before loading), so it is the full conversation to send.
	reply, err := g.agent.Process(ctx, shared.FinanceLedgerID, input.ShortTerm, input.UserMessage.Timestamp)
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
