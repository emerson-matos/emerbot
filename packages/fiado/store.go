package fiado

import (
	"context"

	"github.com/emerson/emerbot/packages/domain"
)

// Page requests a slice of a timeline.
//
// Limit <= 0 means "the whole thing", which is what a total needs: "quanto o
// João me pagou" is the sum of his negatives and a partial sum would be a wrong
// number presented as a right one. A page that *was* cut says so — NextCursor
// is set and callers relay it (ADR-015).
type Page struct {
	Limit int
	// Cursor is opaque and comes from a previous MovementPage.NextCursor.
	// Stores reject one that does not belong to the timeline being read rather
	// than paginating from somewhere else.
	Cursor string
}

// MovementPage is one page of a timeline.
type MovementPage struct {
	Movements []Movement
	// NextCursor is empty when the timeline ended. Non-empty means there are
	// more rows — the caller either fetches them or says out loud that the list
	// is partial.
	NextCursor string
}

// DefaultPageLimit and MaxPageLimit bound a requested page. They are sized for
// a counter's history: a client's whole year fits well inside the maximum, so
// the cap is a backstop rather than a page size somebody has to defeat.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// ClampLimit normalizes a requested page size. A zero or negative request is
// the default rather than "everything": the callers that genuinely want the
// whole timeline pass Page{} straight to the store and never come through here.
func ClampLimit(n int) int {
	if n <= 0 {
		return DefaultPageLimit
	}
	if n > MaxPageLimit {
		return MaxPageLimit
	}
	return n
}

// Store is the caderninho's persistence. It is its own interface, not six more
// methods on finance.Store: the two systems share a table and nothing else, and
// a consumer of one has no business seeing the other (ADR-014).
type Store interface {
	// Record writes one movement and returns the debtor as it stands after it.
	// The two copies of the movement and the balance update are one atomic
	// write — there is no state in which only one copy exists.
	Record(ctx context.Context, m Movement) (Debtor, error)

	// Delete removes a movement and takes its amount back out of the balance,
	// atomically. This is how a mistake is corrected: the wrong line disappears
	// and the right one is recorded after it, the way it works on paper. A
	// compensating line is not an option — the sign is the only type there is,
	// so an "adjustment" would be indistinguishable from money the client paid.
	//
	// The cost is that a correction leaves no trace, which is the same choice
	// the timeline makes by having no audit trail.
	Delete(ctx context.Context, userID string, ref Ref) (Debtor, error)

	// Debtor reads one client's latest, strongly consistent. "fiado 40 do joão"
	// and "quanto o joão me deve?" arrive seconds apart, so a read that could
	// miss the write before it would answer a wrong number confidently.
	// ErrDebtorNotFound means nobody by that slug is in the book.
	Debtor(ctx context.Context, userID, client string) (Debtor, error)

	// ListDebtors is the caderninho itself: every latest, and only the latests.
	// Ordered by slug — the key's own order — so the two implementations cannot
	// disagree; callers that want it by balance sort it themselves.
	ListDebtors(ctx context.Context, userID string) ([]Debtor, error)

	// DayMovements is the caderninho on one day, across every client, in
	// chronological order.
	DayMovements(ctx context.Context, userID string, date domain.CalendarDate, page Page) (MovementPage, error)

	// ClientMovements is one person's statement, most recent first.
	ClientMovements(ctx context.Context, userID, client string, page Page) (MovementPage, error)
}
