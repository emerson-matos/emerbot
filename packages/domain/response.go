package domain

type Response struct {
	Text        string
	UsedLLM     bool
	ToolResults []ToolResult
}
