package tools

import "testing"

func TestNewRegistryPanicsOnDuplicateToolName(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate tool name")
		}
	}()

	NewRegistry(EchoTool{}, EchoTool{})
}
