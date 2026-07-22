package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/emerson/emerbot/packages/domain"
)

type ToolDefinition struct {
	Name        string
	Description string
}

type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, userID string, input string) (domain.ToolResult, error)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(items ...Tool) *Registry {
	registry := &Registry{tools: make(map[string]Tool, len(items))}
	for _, item := range items {
		name := strings.TrimSpace(item.Name())
		if name == "" {
			panic("tool name must not be empty")
		}
		if _, exists := registry.tools[name]; exists {
			panic(fmt.Sprintf("tool %q already registered", name))
		}
		registry.tools[name] = item
	}
	return registry
}

func (r *Registry) Definitions() []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(r.tools))
	for name, tool := range r.tools {
		definitions = append(definitions, ToolDefinition{
			Name:        name,
			Description: tool.Description(),
		})
	}

	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *Registry) Execute(ctx context.Context, call domain.ToolCall, userID string) (domain.ToolResult, error) {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return domain.ToolResult{}, fmt.Errorf("tool name is required")
	}

	tool, ok := r.tools[name]
	if !ok {
		return domain.ToolResult{}, fmt.Errorf("tool %q not registered", name)
	}

	return tool.Execute(ctx, userID, call.Input)
}

type EchoTool struct{}

func (EchoTool) Name() string { return "echo" }

func (EchoTool) Description() string {
	return "ecoar de volta a entrada do usuário"
}

func (EchoTool) Execute(_ context.Context, _ string, input string) (domain.ToolResult, error) {
	return domain.ToolResult{Name: "echo", Output: input}, nil
}
