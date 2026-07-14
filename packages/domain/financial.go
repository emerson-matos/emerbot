package domain

import (
	"encoding/json"
	"time"
)

type (
	EntryType     string
	PaymentStatus string
)

const (
	EntryTypeExpense EntryType = "expense"
	EntryTypeIncome  EntryType = "income"

	PaymentStatusPending PaymentStatus = "pending"
	PaymentStatusPaid    PaymentStatus = "paid"
)

// FinancialEntry represents a single financial transaction for the pharmacy.
// Amount is stored in centavos (R$1,00 = 100) to avoid floating-point issues.
type FinancialEntry struct {
	UserID        string
	EntryID       string
	Date          time.Time
	Amount        int64 // centavos: R$500,00 = 50000
	Category      string
	Type          EntryType
	Description   string
	DueDate       *time.Time
	PaymentStatus PaymentStatus
	PaymentDate   *time.Time
	Supplier      string
	Source        string // "whatsapp" | "manual"
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// RecurrenceID groups occurrences generated together by /recorrente.
	// Empty for one-off entries.
	RecurrenceID    string
	RecurrenceIndex int // 1-based position within the series
	RecurrenceTotal int // total occurrences in the series
}

// SK returns the DynamoDB sort key: ENTRY#<date>#<entryID>
func (e FinancialEntry) SK() string {
	return "ENTRY#" + e.Date.Format("2006-01-02") + "#" + e.EntryID
}

// MarshalJSON serializes Date, DueDate, and PaymentDate as plain
// "YYYY-MM-DD" calendar dates instead of full RFC3339 timestamps. These
// fields represent the day a transaction happened or is due, not a specific
// instant — emitting a time-of-day and "Z"/offset invites API consumers to
// round-trip them through a timezone-aware Date object, which silently
// shifts the displayed day for any viewer behind UTC. CreatedAt/UpdatedAt
// are genuine instants and keep their normal RFC3339 encoding.
func (e FinancialEntry) MarshalJSON() ([]byte, error) {
	type alias FinancialEntry
	return json.Marshal(struct {
		alias
		Date        string  `json:"Date"`
		DueDate     *string `json:"DueDate,omitempty"`
		PaymentDate *string `json:"PaymentDate,omitempty"`
	}{
		alias:       alias(e),
		Date:        e.Date.Format("2006-01-02"),
		DueDate:     formatCalendarDate(e.DueDate),
		PaymentDate: formatCalendarDate(e.PaymentDate),
	})
}

func formatCalendarDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// UnmarshalJSON is the inverse of MarshalJSON: it reads Date, DueDate, and
// PaymentDate as plain "YYYY-MM-DD" calendar dates.
func (e *FinancialEntry) UnmarshalJSON(data []byte) error {
	type alias FinancialEntry
	aux := struct {
		*alias
		Date        string  `json:"Date"`
		DueDate     *string `json:"DueDate,omitempty"`
		PaymentDate *string `json:"PaymentDate,omitempty"`
	}{alias: (*alias)(e)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.Date != "" {
		t, err := time.Parse("2006-01-02", aux.Date)
		if err != nil {
			return err
		}
		e.Date = t
	}
	e.DueDate = parseCalendarDate(aux.DueDate)
	e.PaymentDate = parseCalendarDate(aux.PaymentDate)
	return nil
}

func parseCalendarDate(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return nil
	}
	return &t
}

// Goal represents a monthly financial target (faturamento/teto de despesa).
type Goal struct {
	UserID        string // "pai"
	Month         string // "2026-07"
	RevenueTarget int64  // centavos — meta de faturamento
	ExpenseTarget int64  // centavos — teto de despesa
}

// AmountReais returns the amount formatted as a Brazilian real string.
func (e FinancialEntry) AmountReais() string {
	reais := e.Amount / 100
	centavos := e.Amount % 100
	if centavos == 0 {
		return formatInt(reais) + ",00"
	}
	if centavos < 10 {
		return formatInt(reais) + ",0" + formatInt(centavos)
	}
	return formatInt(reais) + "," + formatInt(centavos)
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}
