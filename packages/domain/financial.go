package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

type (
	EntryID       string
	EntryType     string
	PaymentStatus string
	EntrySource   string
	// IncomeOrigin is where an incoming amount came from. It answers "was this
	// a sale?", which EntryType cannot: every inflow is EntryTypeIncome,
	// whether it was sold, borrowed or contributed by a partner.
	//
	// It is not EntrySource: that one records which *channel* wrote the record
	// (WhatsApp, the dashboard), not where the money came from.
	IncomeOrigin string
)

const (
	EntryTypeExpense EntryType = "expense"
	EntryTypeIncome  EntryType = "income"

	PaymentStatusPending PaymentStatus = "pending"
	PaymentStatusPaid    PaymentStatus = "paid"

	SourceWhatsApp EntrySource = "whatsapp"
	SourceManual   EntrySource = "manual"
	SourceUnknown  EntrySource = "unknown"

	// OriginVenda is the only origin that counts toward faturamento — see
	// IsRevenue. A sale counts when it is *made*, so a crediário sale is
	// OriginVenda on the day it happens, whether or not it has been paid.
	OriginVenda IncomeOrigin = "venda"
	// OriginRecebimentoCliente is a customer settling a debt that was never
	// recorded as a sale in this ledger — a receivable that predates it.
	// Settling a crediário sale that *is* in the ledger is not this: that is
	// the original OriginVenda entry being marked paid. Recording it as a new
	// entry would count the sale twice, which is why this origin is
	// deliberately absent from the AI's create tool (see packages/finance/tools.go).
	OriginRecebimentoCliente IncomeOrigin = "recebimento_cliente"
	OriginEmprestimo         IncomeOrigin = "emprestimo"
	OriginAporteSocio        IncomeOrigin = "aporte_socio"
	OriginReceitaFinanceira  IncomeOrigin = "receita_financeira"
	OriginRestituicao        IncomeOrigin = "restituicao"
	OriginOutros             IncomeOrigin = "outros"
)

// MaxPaymentMethodLen bounds FinancialEntry.PaymentMethod at the edges that
// accept one — the dashboard API and the agent's tools. Validate deliberately
// does not enforce it (see ADR-026): it runs on every read, so a stored value
// that somehow got past an edge must not turn a whole ListEntries into an
// error. A label longer than this is not a means of payment, it is a sentence.
const MaxPaymentMethodLen = 60

// CleanPaymentMethod trims s and reports whether it is short enough to accept.
// Both edges use it so "pix " and "pix" are never two different answers, and so
// the limit is one number rather than one per handler.
func CleanPaymentMethod(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, utf8.RuneCountInString(s) <= MaxPaymentMethodLen
}

func NormalizeSource(s string) EntrySource {
	switch EntrySource(s) {
	case SourceWhatsApp, SourceManual, SourceUnknown:
		return EntrySource(s)
	default:
		return SourceUnknown
	}
}

// IncomeOrigins returns every valid origin, in the order the UI offers them.
func IncomeOrigins() []IncomeOrigin {
	return []IncomeOrigin{
		OriginVenda,
		OriginRecebimentoCliente,
		OriginEmprestimo,
		OriginAporteSocio,
		OriginReceitaFinanceira,
		OriginRestituicao,
		OriginOutros,
	}
}

// OriginLabels maps each origin to its pt-BR label, so the dashboard, the
// WhatsApp replies and the AI tool schemas all name them the same way.
func OriginLabels() map[IncomeOrigin]string {
	return map[IncomeOrigin]string{
		OriginVenda:              "Venda",
		OriginRecebimentoCliente: "Recebimento de cliente",
		OriginEmprestimo:         "Empréstimo",
		OriginAporteSocio:        "Aporte de sócio",
		OriginReceitaFinanceira:  "Receita financeira",
		OriginRestituicao:        "Restituição",
		OriginOutros:             "Outros",
	}
}

// NormalizeIncomeOrigin coerces s to a known origin, falling back to
// OriginOutros. An empty string stays empty: "not recorded" is a real state
// that IsRevenue handles, and turning it into a value here would erase the
// distinction between an origin nobody set and one somebody chose.
func NormalizeIncomeOrigin(s string) IncomeOrigin {
	if s == "" {
		return ""
	}
	for _, o := range IncomeOrigins() {
		if IncomeOrigin(s) == o {
			return o
		}
	}
	return OriginOutros
}

// IsRevenue reports whether an entry counts toward faturamento: money earned
// by selling something, as opposed to money that merely came in (a loan, a
// partner's capital, an investment yield, a tax refund). It is what the goals,
// projections, weekday averages, week-over-week pace and every other
// performance indicator are measured against — "empréstimo não é faturamento".
//
// Entradas de caixa, the broader figure, is every income entry that was
// actually received; see packages/finance.CashInTotal.
//
// Origin alone decides. An income entry with no origin — only reachable for a
// row written before the field existed and never migrated — is not counted as a
// sale. The old category fallback (the migration shim) is gone now that
// scripts/migrate-origin has run: an empty origin can only be a stray, and
// counting a stray as a sale is exactly the mistake this function prevents.
func IsRevenue(e FinancialEntry) bool {
	return e.Type == EntryTypeIncome && e.Origin == OriginVenda
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
	// Origin is where the money came from, for income entries; empty on
	// expenses. Empty on an income entry means a legacy row the backfill never
	// reached — IsRevenue treats it as not-a-sale.
	Origin IncomeOrigin
	// PaymentMethod is how this was paid or received, in the user's own words
	// ("pix", "dinheiro", "cartão da loja") — see ADR-026. Free text on purpose:
	// it exists so the person reading their own ledger recognises the movement,
	// not so the system can total anything by it. Nothing here or downstream
	// groups, filters or sums by this field, and turning it into an enum would
	// be a migration, not a cleanup.
	//
	// It is a fact about the settlement, so it lives with PaymentDate: Normalize
	// clears both when an entry goes back to pending. Empty is the ordinary
	// state — every entry written before the field existed has none, and filling
	// one in is always optional.
	PaymentMethod string
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// RecurrenceID groups occurrences created together as one series. Empty
	// for one-off entries, which is every entry written since the /recorrente
	// command was removed — the fields stay so the series already in the
	// ledger keep rendering as series.
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
	Origin          IncomeOrigin
	PaymentMethod   string
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
		Type: input.Type, PaymentStatus: input.PaymentStatus, Source: input.Source, Origin: input.Origin,
		PaymentMethod: input.PaymentMethod,
		CreatedAt:     now, UpdatedAt: now,
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
		// The means of payment is a fact about the settlement, so it goes with
		// the date it happened on. Un-paying a bill that was settled "no pix"
		// and keeping the "pix" would leave the ledger saying how something was
		// paid that nobody paid.
		e.PaymentMethod = ""
	}
	e.PaymentMethod = strings.TrimSpace(e.PaymentMethod)
	// An origin on an expense is meaningless — "where did this money come
	// from" has no answer for money going out.
	if e.Type == EntryTypeExpense {
		e.Origin = ""
	}
	// Deliberately no default for an empty origin on an income entry. Filling
	// it here would run before Validate on every read (see
	// packages/finance.itemToEntry) and silently relabel every entry written
	// before the field existed — a loan filed under "Outros (Receita)" would
	// come back as a sale. IsRevenue handles the empty case explicitly instead.
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
	// An empty origin is still allowed on income, on purpose: itemToEntry runs
	// Validate on every read, so requiring an origin would turn a single stray
	// un-migrated row into a failure of the whole ListEntries query. IsRevenue
	// already treats an empty origin as not-a-sale, so a stray costs a bit of
	// under-counted faturamento, never a broken read. Tighten to require a value
	// only once the ledger is provably free of origin-less rows.
	if e.Origin != "" {
		if e.Type != EntryTypeIncome {
			return errors.New("expense entry cannot have an income origin")
		}
		if NormalizeIncomeOrigin(string(e.Origin)) != e.Origin {
			return errors.New("invalid income origin")
		}
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

// Goal represents a monthly financial target: how much to sell, and how much
// to spend at most.
//
// RevenueTarget is a faturamento target — measured against sales only (see
// IsRevenue), never against entradas de caixa. A month that hits its number
// because of a loan has not hit its number. The name also matches the stored
// DynamoDB attribute again (see packages/finance.goalItem).
type Goal struct {
	UserID        string
	Month         string
	RevenueTarget int64
	ExpenseTarget int64
}

// There is deliberately no money formatter on the domain type. The one that
// lived here rendered R$ 1.000,00 as "1000,00" — no thousands separator at all
// — and was used only by the slash-command confirmations, beside a second
// formatter that wrote the same amount a different way. Rendering is the edge's
// job: analytics.formatBRL for anything the backend writes, Intl.NumberFormat
// in the browser (apps/web/src/lib/format.ts).
