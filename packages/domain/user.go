package domain

// User represents an authenticated user of the dashboard.
type User struct {
	UserID       string
	Email        string
	PasswordHash string // bcrypt hash
	Name         string
}
