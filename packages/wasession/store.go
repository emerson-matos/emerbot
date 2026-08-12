// Package wasession tracks WhatsApp's customer-service window. The webhook
// records when a phone last messaged the business; the scheduled notifier only
// sends free-form messages while the window is open. Records self-expire via a
// TTL shorter than WhatsApp's real 24h limit (see Window) so the daily job
// never fires near the boundary.
package wasession

import (
	"context"
	"time"
)

// Window is how long a session stays active after an inbound message. It is
// deliberately below WhatsApp's 24h limit (a safety margin for the daily
// notifier's timing and clock skew) and is also the DynamoDB TTL on each record.
const Window = 20 * time.Hour

// DedupWindow is how long a processed message ID is remembered so a redelivery
// (the queue's, or Meta's own retry behind it) is ignored. It comfortably
// exceeds both retry spans so a duplicate can never slip through after expiry.
const DedupWindow = 48 * time.Hour

// dedupKeyPrefix namespaces message-dedup items so their hash key can never
// collide with a phone number (which is all digits) in the shared table.
const dedupKeyPrefix = "MSGID#"

// Store persists the "phone last messaged us" signal behind the 24h window.
type Store interface {
	// RecordInbound marks that phone messaged us at `at`; the session is then
	// active until at+Window.
	RecordInbound(ctx context.Context, phone string, at time.Time) error
	// Active reports whether phone's session is still open as of `now`.
	Active(ctx context.Context, phone string, now time.Time) (bool, error)
	// ActiveUntil reports when phone's window closes, which is what a caller
	// needs to explain a non-delivery: "the window shut at 14:20" and "this
	// phone never messaged us" are different problems with different fixes, and
	// a bare Active cannot tell them apart.
	//
	// A zero time means no session record exists. Because records self-expire
	// via TTL, that covers both "never messaged us" and "messaged us long
	// enough ago that the record is gone" — indistinguishable by design.
	ActiveUntil(ctx context.Context, phone string) (time.Time, error)
	// Processed answers the domain's question — "já respondemos esta mensagem?"
	// — as of now. The worker asks it before doing any work, so a redelivery
	// costs a read instead of a second Gemini turn and a second answer.
	//
	// It is a different question from the queue's transport dedup, which only
	// covers "esta mensagem chegou duas vezes agora" and expires with a window
	// that belongs to SQS (ADR-029). An empty messageID is never processed
	// (there is nothing to remember it by).
	Processed(ctx context.Context, messageID string, now time.Time) (bool, error)
	// MarkProcessed records that messageID has been handled and reports whether
	// this call is the one that recorded it (false = someone got there first).
	//
	// It is written when the turn *ends*, not when it starts: the mark is the
	// fact that the message was answered, not a reservation that a failed turn
	// would then have to give back. The compensating Unmark it replaces never
	// worked in production anyway — the policy had no dynamodb:DeleteItem, so
	// every retry was answered "ignoring duplicate" (ADR-029).
	//
	// An empty messageID always returns true (nothing to dedup on).
	MarkProcessed(ctx context.Context, messageID string, now time.Time) (bool, error)
}
