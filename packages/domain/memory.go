package domain

type Memory struct {
	UserID string
	Type   string
	ID     string
	Value  string
}

func (m Memory) Key() string {
	return m.Type + "#" + m.ID
}

