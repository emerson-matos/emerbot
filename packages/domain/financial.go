package domain

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

type (
	EntryID       string
	EntryType     string
	PaymentStatus string
	EntrySource   string
)

const (
	EntryTypeExpense EntryType = "expense"
	EntryTypeIncome  EntryType = "income"

	PaymentStatusPending PaymentStatus = "pending"
	PaymentStatusPaid    PaymentStatus = "paid"

	SourceWhatsApp EntrySource = "whatsapp"
	SourceManual   EntrySource = "manual"
	SourceUnknown  EntrySource = "unknown"
)

func NormalizeSource(s string) EntrySource {
	switch EntrySource(s) {
	case SourceWhatsApp, SourceManual, SourceUnknown:
		return EntrySource(s)
	default:
		return SourceUnknown
	}
}

// FinancialEntry represents a single financial transaction for the pharmacy.
// Amount is stored in centavos (R$1,00 = 100) to avoid floating-point issues.
type FinancialEntry struct {
	UserID          string
	EntryID         EntryID
	TransactionDate CalendarDate
	DueDate         *CalendarDate
	PaymentDate     *CalendarDate
	Amount          int64
	Category        string
	Description     string
	Supplier        string
	Type            EntryType
	PaymentStatus   PaymentStatus
	Source          EntrySource
	CreatedAt       time.Time
	UpdatedAt       time.Time

	// RecurrenceID groups occurrences generated together by /recorrente.
	// Empty for one-off entries.
	RecurrenceID    string
	RecurrenceIndex int // 1-based position within the series
	RecurrenceTotal int // total occurrences in the series
}

// NewFinancialEntryInput contains the caller-provided values for a new entry.
type NewFinancialEntryInput struct {
	UserID          string
	TransactionDate CalendarDate
	DueDate         *CalendarDate
	PaymentDate     *CalendarDate
	Amount          int64
	Category        string
	Description     string
	Supplier        string
	Type            EntryType
	PaymentStatus   PaymentStatus
	Source          EntrySource
	RecurrenceID    string
	RecurrenceIndex int
	RecurrenceTotal int
}

// NewFinancialEntry creates a valid entry with a ULID and UTC audit times.
func NewFinancialEntry(input NewFinancialEntryInput) (FinancialEntry, error) {
	now := time.Now().UTC()
	e := FinancialEntry{
		UserID: input.UserID, EntryID: EntryID(ulid.Make().String()),
		TransactionDate: input.TransactionDate, DueDate: input.DueDate, PaymentDate: input.PaymentDate,
		Amount: input.Amount, Category: input.Category, Description: input.Description, Supplier: input.Supplier,
		Type: input.Type, PaymentStatus: input.PaymentStatus, Source: input.Source,
		CreatedAt: now, UpdatedAt: now,
		RecurrenceID: input.RecurrenceID, RecurrenceIndex: input.RecurrenceIndex, RecurrenceTotal: input.RecurrenceTotal,
	}
	e.Normalize()
	return e, nil
}

// Normalize fixes common data inconsistencies (self-heal for data read from
// external storage that may predate the normalization in NewFinancialEntry).
func (e *FinancialEntry) Normalize() {
	if e.PaymentStatus == PaymentStatusPaid && e.PaymentDate == nil {
		date := e.TransactionDate
		e.PaymentDate = &date
	}
	if e.PaymentStatus == PaymentStatusPending {
		e.PaymentDate = nil
	}
}

// Validate checks financial invariants before an entry is persisted.
func (e FinancialEntry) Validate() error {
	if e.UserID == "" {
		return errors.New("user id is required")
	}
	if e.EntryID == "" {
		return errors.New("entry id is required")
	}
	if e.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	if !e.TransactionDate.Valid() {
		return errors.New("transaction date is required")
	}
	switch e.Type {
	case EntryTypeExpense, EntryTypeIncome:
	default:
		return errors.New("invalid entry type")
	}
	switch e.Source {
	case SourceWhatsApp, SourceManual, SourceUnknown:
	default:
		return errors.New("invalid entry source")
	}
	switch e.PaymentStatus {
	case PaymentStatusPending:
		if e.PaymentDate != nil {
			return errors.New("pending entry cannot have payment date")
		}
	case PaymentStatusPaid:
		if e.PaymentDate == nil {
			return errors.New("paid entry requires payment date")
		}
	default:
		return errors.New("invalid payment status")
	}
	return nil
}

// Goal represents a monthly financial target (faturamento/teto de despesa).
type Goal struct {
	UserID        string
	Month         string
	RevenueTarget int64
	ExpenseTarget int64
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
