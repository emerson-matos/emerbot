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

// Store persists the "phone last messaged us" signal behind the 24h window.
type Store interface {
	// RecordInbound marks that phone messaged us at `at`; the session is then
	// active until at+Window.
	RecordInbound(ctx context.Context, phone string, at time.Time) error
	// Active reports whether phone's session is still open as of `now`.
	Active(ctx context.Context, phone string, now time.Time) (bool, error)
}
