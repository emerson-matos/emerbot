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

// DedupWindow is how long a processed message ID is remembered so WhatsApp
// retries (which re-deliver the same message ID) are ignored. It comfortably
// exceeds Meta's retry span so a duplicate can never slip through after expiry.
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
	// MarkProcessed records that messageID has been handled and reports whether
	// this is the first time it was seen (true = process it; false = a retry to
	// ignore). An empty messageID always returns true (nothing to dedup on).
	MarkProcessed(ctx context.Context, messageID string, now time.Time) (bool, error)
	// Unmark removes a message's dedup marker. It compensates a MarkProcessed
	// whose turn then failed without a 2xx, so WhatsApp's retry is reprocessed
	// instead of being swallowed as a duplicate. An empty messageID is a no-op.
	Unmark(ctx context.Context, messageID string) error
}
