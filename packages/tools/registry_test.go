package tools

import (
	"context"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

func TestNewRegistryPanicsOnDuplicateToolName(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate tool name")
		}
	}()

	NewRegistry(EchoTool{}, EchoTool{})
}

func TestNewRegistryPanicsOnBlankToolName(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on blank tool name")
		}
	}()

	NewRegistry(blankTool{})
}

type blankTool struct{}

func (blankTool) Name() string {
	return "   "
}

func (blankTool) Description() string {
	return "blank"
}

func (blankTool) Execute(_ context.Context, _ string, _ string) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}
