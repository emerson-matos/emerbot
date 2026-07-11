package tools

import (
	"context"
	"fmt"
	"sort"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(items ...Tool) *Registry {
	registry := &Registry{tools: make(map[string]Tool, len(items))}
	for _, item := range items {
		registry.tools[item.Name()] = item
	}
	return registry
}

func (r *Registry) Definitions() []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		definitions = append(definitions, llm.ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
		})
	}

	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}

func (r *Registry) Execute(ctx context.Context, call domain.ToolCall, userID string) (domain.ToolResult, error) {
	tool, ok := r.tools[call.Name]
	if !ok {
		return domain.ToolResult{}, fmt.Errorf("tool %q not registered", call.Name)
	}

	return tool.Execute(ctx, userID, call.Input)
}

