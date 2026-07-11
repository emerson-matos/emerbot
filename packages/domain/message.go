package domain

import "time"

// Message is the normalized internal contract used by the application.
type Message struct {
	UserID    string
	Text      string
	Timestamp time.Time
	MessageID string
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

type ConversationMessage struct {
	Role      Role
	Text      string
	Timestamp time.Time
}
