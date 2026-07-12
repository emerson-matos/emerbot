package domain

import "time"

type EntryType string
type PaymentStatus string

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
}

// SK returns the DynamoDB sort key: ENTRY#<date>#<entryID>
func (e FinancialEntry) SK() string {
	return "ENTRY#" + e.Date.Format("2006-01-02") + "#" + e.EntryID
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
