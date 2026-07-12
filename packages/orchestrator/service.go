package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/tools"
)

type Service struct {
	llm              llm.Client
	shortTerm        memory.ShortTermStore
	longTerm         memory.LongTermStore
	tools            *tools.Registry
	shortTermLimit   int
	defaultResponder string
}

func NewService(client llm.Client, shortTerm memory.ShortTermStore, longTerm memory.LongTermStore, registry *tools.Registry) *Service {
	return &Service{
		llm:              client,
		shortTerm:        shortTerm,
		longTerm:         longTerm,
		tools:            registry,
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

	output, err := s.llm.Generate(ctx, llm.Input{
		UserMessage:  message,
		ShortTerm:    shortTerm,
		LongTerm:     longTerm,
		Available:    s.tools.Definitions(),
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
