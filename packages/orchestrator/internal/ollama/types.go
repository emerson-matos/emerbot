package ollama

// Wire types for Ollama's POST /api/chat (non-streaming). Only the fields the
// agent needs are modeled. See https://github.com/ollama/ollama/blob/main/docs/api.md

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Tools    []tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
	Options  options   `json:"options"`
}

type options struct {
	Temperature float32 `json:"temperature"`
}

type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	// ToolName labels a role:"tool" result with the function it answers (newer
	// Ollama). Harmless to older servers, which ignore it.
	ToolName string `json:"tool_name,omitempty"`
}

type toolCall struct {
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name string `json:"name"`
	// Ollama returns arguments as a JSON object, not a stringified one.
	Arguments map[string]any `json:"arguments"`
}

type tool struct {
	Type     string       `json:"type"` // always "function"
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

type chatResponse struct {
	Model   string  `json:"model"`
	Message message `json:"message"`
	Done    bool    `json:"done"`
}
