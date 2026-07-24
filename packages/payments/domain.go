// Package payments holds the provider-agnostic canonical domain for imported
// payment-processor data (PagBank today, Stone later) plus the persistence and
// import-orchestration seams. Nothing here knows a provider's wire format — that
// lives in the per-provider parser subpackages (e.g. packages/payments/pagbank).
//
// Architecture principles (see docs/plan): the canonical domain is
// provider-agnostic; parsers are pure deterministic translators; ImportService
// only orchestrates; the Repository owns persistence and the replace strategy;
// one import produces one ImportResult (single Provider + single SourceDate).
package payments

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/emerson/emerbot/packages/domain"
)

// Provider identifies a payment processor. Kept as a typed enum so a typo is a
// compile error, not a silent runtime mismatch.
type Provider string

const (
	ProviderPagBank Provider = "pagbank"
	ProviderStone   Provider = "stone"
)

// Valid reports whether p is a known provider.
func (p Provider) Valid() bool {
	switch p {
	case ProviderPagBank, ProviderStone:
		return true
	default:
		return false
	}
}

// PaymentMethod is the canonical means of payment, mapped from each provider's
// own codes by its parser.
type PaymentMethod string

const (
	MethodCredito PaymentMethod = "credito"
	MethodDebito  PaymentMethod = "debito"
	MethodPix     PaymentMethod = "pix"
	MethodBoleto  PaymentMethod = "boleto"
	MethodOutros  PaymentMethod = "outros"
)

// SaleID is the provider-qualified identifier of a sale, "<provider>:<externalID>"
// (e.g. "pagbank:ABC123"), so ids from different providers can never collide and
// receivable/payment links stay type-safe.
type SaleID string

// NewSaleID builds a SaleID from a provider and the provider's own external id.
func NewSaleID(provider Provider, externalID string) SaleID {
	return SaleID(string(provider) + ":" + externalID)
}

// Sale is one commercial sale. Amounts are in centavos (R$1,00 = 100), matching
// the rest of the app (see packages/domain/financial.go). Fee is part of the
// sale, not its own entity: FeeAmount = GrossAmount - NetAmount.
type Sale struct {
	ID           SaleID
	Provider     Provider
	ExternalID   string
	SaleDate     domain.CalendarDate
	GrossAmount  int64
	NetAmount    int64
	FeeAmount    int64
	Method       PaymentMethod
	Brand        string
	Installments int
}

// ExpectedReceivable is money expected to become available in the future — one
// per installment of a sale. This is what the cash-flow forecast consumes.
type ExpectedReceivable struct {
	Provider          Provider
	SaleID            SaleID
	ExpectedDate      domain.CalendarDate
	Amount            int64
	InstallmentNumber int
	InstallmentTotal  int
}

// Payment is money that actually became available (a liquidation).
type Payment struct {
	Provider    Provider
	SaleID      SaleID
	PaymentDate domain.CalendarDate
	Amount      int64
}

// Validate checks a sale's invariants before persistence.
func (s Sale) Validate() error {
	if !s.Provider.Valid() {
		return fmt.Errorf("invalid provider %q", s.Provider)
	}
	if s.ExternalID == "" {
		return errors.New("sale external id is required")
	}
	if s.ID == "" {
		return errors.New("sale id is required")
	}
	if !s.SaleDate.Valid() {
		return errors.New("sale date is required")
	}
	if s.GrossAmount < 0 || s.NetAmount < 0 {
		return errors.New("sale amounts must be non-negative")
	}
	return nil
}

// Validate checks a receivable's invariants before persistence.
func (r ExpectedReceivable) Validate() error {
	if !r.Provider.Valid() {
		return fmt.Errorf("invalid provider %q", r.Provider)
	}
	if r.SaleID == "" {
		return errors.New("receivable sale id is required")
	}
	if !r.ExpectedDate.Valid() {
		return errors.New("receivable expected date is required")
	}
	if r.InstallmentNumber <= 0 {
		return errors.New("receivable installment number must be positive")
	}
	return nil
}

// Validate checks a payment's invariants before persistence.
func (p Payment) Validate() error {
	if !p.Provider.Valid() {
		return fmt.Errorf("invalid provider %q", p.Provider)
	}
	if p.SaleID == "" {
		return errors.New("payment sale id is required")
	}
	if !p.PaymentDate.Valid() {
		return errors.New("payment date is required")
	}
	return nil
}

// ParseCentavos converts a decimal-reais string (EDI numeric, "." decimal
// separator, e.g. "108.96") to integer centavos, rounding half-up on any digits
// beyond the second so no centavo is silently truncated. It parses the integer
// and fractional parts directly rather than going through float64.
func ParseCentavos(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	intStr, fracStr, _ := strings.Cut(s, ".")
	if intStr == "" {
		intStr = "0"
	}
	reais, err := strconv.ParseInt(intStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse reais %q: %w", s, err)
	}

	// Normalize the fractional part to exactly two digits, rounding half-up.
	var centavos int64
	switch {
	case fracStr == "":
		centavos = 0
	case len(fracStr) == 1:
		d, err := strconv.ParseInt(fracStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse centavos %q: %w", s, err)
		}
		centavos = d * 10
	default:
		two, err := strconv.ParseInt(fracStr[:2], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse centavos %q: %w", s, err)
		}
		centavos = two
		if len(fracStr) > 2 && fracStr[2] >= '5' { // round half-up on the third digit
			centavos++
		}
	}

	total := reais*100 + centavos
	if neg {
		total = -total
	}
	return total, nil
}
