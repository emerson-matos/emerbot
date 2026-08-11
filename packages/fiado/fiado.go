// Package fiado is the caderninho: who owes the pharmacy, how much, and since
// when.
//
// It is deliberately a system of its own, not an extension of packages/finance
// (ADR-027). A fiado sale is not faturamento, not caixa and not a receivable
// with a due date — nothing was promised, so nothing can be late. Its records
// are their own items in the user's partition and they reach no summary, no
// projection and no notification. Nothing here imports packages/finance, and
// that is the point: the moment the caderninho can move a metric, it stops
// being a caderninho.
//
// The vocabulary is part of the design. A debt is "em aberto há N dias",
// counted from the day the balance left zero. Never "vencido", never
// "atrasado": fiado does not fall due, it ages.
package fiado

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"

	"github.com/emerson/emerbot/packages/domain"
)

// Movement is one line of the caderninho: something taken, or something paid.
//
// The sign is the only type there is. A positive Amount is debt, a negative one
// is a payment — so "quanto o João me pagou" is the sum of his negatives, and a
// purchase and a settlement are the same write with the sign flipped. There is
// no Kind field and none may be added without a key change: a compensating
// "adjustment" line posted as a payment would count money that never arrived
// (see Delete, which is how a mistake is fixed).
type Movement struct {
	UserID string
	// Client is the debtor's slug, through domain.NormalizeCategorySlug — the
	// one slug form this codebase has. It is what the keys are built from.
	Client string
	// Name is the debtor as the user typed them, copied onto the movement so a
	// day's timeline can be rendered without reading one latest per row.
	Name string
	// ID is a ULID. It is what keeps two movements of the same client on the
	// same day — routine at a counter — from overwriting each other.
	ID          string
	Date        domain.CalendarDate
	Amount      int64 // centavos, signed: positive is debt, negative is payment
	Description string
	CreatedAt   time.Time
}

// Debtor is the latest: the photograph of a client after their last movement.
//
// It is a cache, never the source. The balance *is* the sum of that client's
// movements; this exists because "quanto o fulano me deve agora" is the hottest
// read in the caderninho and a history scan is not an answer to it. If the two
// ever disagree, the movements are right and this is the bug.
type Debtor struct {
	UserID string
	Client string
	Name   string
	// Balance is centavos. Positive is owed to the pharmacy; negative is the
	// client's credit — they paid more than they owed, which is a real thing
	// that happens and is recorded rather than refused.
	Balance int64
	// Since is the day the balance left zero, cleared as soon as there is no
	// debt left to date from (see clearSince — a credit counts as settled).
	// It is not "the oldest open purchase": the two diverge as soon as a client
	// settles and buys again, and this is the one that answers "há quanto tempo
	// esse cliente me deve" without reading a single movement.
	Since     *domain.CalendarDate
	UpdatedAt time.Time
}

var (
	// ErrNoClient refuses a movement nobody is attached to. "Fiado de quem?" is
	// a one-word question, and an anonymous debt is unrecoverable.
	ErrNoClient = errors.New("fiado sem cliente")
	// ErrZeroAmount refuses a movement of nothing: it would take a line in the
	// timeline and move no balance.
	ErrZeroAmount = errors.New("valor do fiado não pode ser zero")
	// ErrAmountTooLarge refuses an absurd figure. Tool arguments are generated
	// from user text by an LLM, and a hallucinated extra zero is cheaper to
	// reject than to explain later.
	ErrAmountTooLarge = errors.New("valor do fiado fora do limite")
	// ErrDebtorNotFound is a client with no line in this user's caderninho, as
	// opposed to a client whose balance happens to be zero.
	ErrDebtorNotFound = errors.New("cliente não está no caderninho")
	// ErrMovementNotFound is a movement that is not there, as opposed to one
	// that could not be read — the same distinction finance.ErrEntryNotFound
	// exists for.
	ErrMovementNotFound = errors.New("movimento não encontrado")
)

// MaxAmountCentavos bounds one movement at R$ 1.000.000,00. A caderninho line
// is a counter purchase; anything past this is a typo or a hallucination.
const MaxAmountCentavos int64 = 1_000_000_00

// MaxNameLen and MaxDescriptionLen bound the two free-text fields. Both end up
// in a DynamoDB item, and the name also seeds a sort key component.
const (
	MaxNameLen        = 60
	MaxDescriptionLen = 200
)

// ClientSlug renders a person's name as the key the caderninho files them
// under. It is domain.NormalizeCategorySlug — the same normalization
// categories go through — because a system with two slug forms has two
// spellings of everything, and here that would mean two debtors.
func ClientSlug(name string) string {
	return domain.NormalizeCategorySlug(name)
}

// NewMovement builds a movement from what a caller heard: a name as typed, a
// signed amount in centavos, a day and a note. It generates the ULID and the
// slug, so no caller has to know how either is made.
func NewMovement(userID, name string, amount int64, date domain.CalendarDate, description string) (Movement, error) {
	name = strings.Join(strings.Fields(name), " ")
	slug := ClientSlug(name)
	if slug == "" {
		return Movement{}, ErrNoClient
	}
	if utf8.RuneCountInString(name) > MaxNameLen {
		return Movement{}, fmt.Errorf("nome do cliente muito longo (máximo %d caracteres)", MaxNameLen)
	}
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > MaxDescriptionLen {
		return Movement{}, fmt.Errorf("descrição muito longa (máximo %d caracteres)", MaxDescriptionLen)
	}

	m := Movement{
		UserID:      userID,
		Client:      slug,
		Name:        name,
		ID:          ulid.Make().String(),
		Date:        date,
		Amount:      amount,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}
	return m, m.Validate()
}

// Validate is what every store runs before writing, so an in-memory store
// cannot accept a movement DynamoDB would refuse.
func (m Movement) Validate() error {
	if strings.TrimSpace(m.UserID) == "" {
		return errors.New("user id is required")
	}
	if m.Client == "" {
		return ErrNoClient
	}
	if m.ID == "" {
		return errors.New("movement id is required")
	}
	if !m.Date.Valid() {
		return errors.New("data do movimento é obrigatória")
	}
	if m.Amount == 0 {
		return ErrZeroAmount
	}
	if m.Amount > MaxAmountCentavos || m.Amount < -MaxAmountCentavos {
		return ErrAmountTooLarge
	}
	return nil
}

// Ref addresses one movement. All three parts are needed because the movement
// is stored twice, and the two sort keys are built from the same three fields
// in a different order.
type Ref struct {
	Client string
	Date   domain.CalendarDate
	ID     string
}

// Ref is the address of this movement.
func (m Movement) Ref() Ref {
	return Ref{Client: m.Client, Date: m.Date, ID: m.ID}
}

// Validate reports whether this reference can address a row at all.
func (r Ref) Validate() error {
	if r.Client == "" {
		return ErrNoClient
	}
	if r.ID == "" {
		return errors.New("movement id is required")
	}
	if !r.Date.Valid() {
		return errors.New("data do movimento é obrigatória")
	}
	return nil
}

// DaysOpen is how long a debtor has been owing, in the only vocabulary this
// feature has: "em aberto há N dias".
//
// It is nil rather than zero when there is nothing to age — a settled client, a
// client in credit (clearSince: a non-positive balance is not a debt), or a debt
// that started today. The last one is ADR-017: today is not a measurable day, so
// the count starts at 1. A zero here would be read as "in the clear", which is
// the opposite of what a debt opened this morning means.
func DaysOpen(d Debtor, today domain.CalendarDate) *int {
	if clearSince(d.Balance) || d.Since == nil || !d.Since.Valid() || !today.Valid() {
		return nil
	}
	days := int(today.UTC().Sub(d.Since.UTC()).Hours() / 24)
	if days < 1 {
		return nil
	}
	return &days
}

// Totals splits a client's history into what they took and what they paid.
//
// The sign is what separates them, and nothing else can: a caderninho with a
// "kind" field would let a correction be posted as a payment and inflate what
// the client has paid. Both figures come back positive, because that is how
// they are spoken.
func Totals(movements []Movement) (taken, paid int64) {
	for _, m := range movements {
		if m.Amount > 0 {
			taken += m.Amount
			continue
		}
		paid -= m.Amount
	}
	return taken, paid
}

// Sum is the balance the movements add up to. It is the definition of a
// debtor's balance; Debtor.Balance is only a cache of it (see Debtor).
func Sum(movements []Movement) int64 {
	var total int64
	for _, m := range movements {
		total += m.Amount
	}
	return total
}

// sinceFromMovements replays a client's history, oldest first, and returns the
// day their balance last left zero — the definition of Debtor.Since, recovered
// from the movements that are its truth.
//
// It is the repair path, not the read path: the latest carries Since precisely
// so the caderninho fits in one Query. This runs when a deletion re-opens a
// balance that had been settled, which is the one case where the day is no
// longer written down anywhere.
func sinceFromMovements(movements []Movement) *domain.CalendarDate {
	var running int64
	var since *domain.CalendarDate
	for _, m := range movements {
		was := running
		running += m.Amount
		switch {
		case clearSince(running):
			since = nil
		case clearSince(was):
			date := m.Date
			since = &date
		}
	}
	return since
}

// oldestFirst reverses a most-recent-first timeline, which is the order the
// replay above needs.
func oldestFirst(movements []Movement) []Movement {
	out := make([]Movement, len(movements))
	for i, m := range movements {
		out[len(movements)-1-i] = m
	}
	return out
}

// SimilarClients returns the debtors already in the caderninho whose slug looks
// like the one just typed, so a caller can ask instead of inventing a second
// person.
//
// This is the whole reason the caderninho works. Nobody spells a name the same
// way twice: "joão", "João Silva" and "Joao S." normalize to three slugs, and
// three debtors where there is one person means the answer to "quanto o João me
// deve" is short — which is exactly the failure that makes somebody stop using
// it. So a name that is not in the book but resembles one that is does not
// create anybody; it comes back as a question (see the create-before-asking
// pattern in packages/finance's errUnknownCategory).
//
// The rule is deliberately narrow: one slug has to be the *abbreviation* of the
// other — same tokens, in the same positions, each one either equal or a
// prefix. "joao" and "joao_s" are both abbreviations of "joao_silva"; "Silva,
// João" and "Silva, Maria" are not each other's, and a rule built on any shared
// token would have made every Silva a candidate for every other. A tool that
// asks about everything is as useless as one that asks about nothing.
func SimilarClients(slug string, debtors []Debtor) []Debtor {
	if slug == "" {
		return nil
	}
	typed := strings.Split(slug, "_")
	var out []Debtor
	for _, d := range debtors {
		if d.Client == slug {
			continue
		}
		if abbreviates(typed, strings.Split(d.Client, "_")) {
			out = append(out, d)
		}
	}
	return out
}

// abbreviates reports whether one token list is the other written short: the
// shorter list must match the longer one token for token, from the first.
func abbreviates(a, b []string) bool {
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) == 0 {
		return false
	}
	for i, tok := range short {
		// A bare initial is only meaningful once something before it matched:
		// "s" is the Silva in "João S." but "j" on its own is half the book. So
		// the first token needs three characters to count as a prefix, and the
		// ones after it do not.
		minPrefix := 1
		if i == 0 {
			minPrefix = 3
		}
		if !tokenAlike(tok, long[i], minPrefix) {
			return false
		}
	}
	return true
}

// tokenAlike matches an equal token, or one that is the start of the other
// ("jo" for "joao") when it is at least minPrefix long.
func tokenAlike(a, b string, minPrefix int) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	return len(short) >= minPrefix && strings.HasPrefix(long, short)
}
