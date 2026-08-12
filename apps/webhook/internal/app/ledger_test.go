package app

import (
	"context"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/wasession"
)

// The sender's phone never becomes a ledger key: every message reaches the
// finance tools through the agent, which is called with shared.FinanceLedgerID.
// That invariant is asserted where it now lives, in
// packages/orchestrator.TestGeminiGeneratorUsesSharedLedgerRegardlessOfSender —
// the webhook used to enforce it separately for slash commands, and those are
// gone. The agent itself moved out of this app entirely (ADR-028); what is
// left to assert here is the 24h window, which stayed.

// TestHandleRecordsInboundMessage proves every inbound message opens the
// WhatsApp 24h window: after handling one, the sender's phone has a recorded
// last-inbound timestamp the notifier can later check.
func TestHandleRecordsInboundMessage(t *testing.T) {
	t.Parallel()

	sessions := wasession.NewInMemoryStore()
	app := New(&fakePublisher{}, &fakeWhatsAppClient{}, "secret", "verify", sessions)

	when := time.Now().UTC().Add(-time.Hour)
	if _, err := app.Handle(context.Background(), Request{
		UserID:    "5511999999999",
		MessageID: "m1",
		Text:      "bom dia",
		Timestamp: when.Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}

	// The message opened the 24h window, so the phone's session is active now.
	active, err := sessions.Active(context.Background(), "5511999999999", time.Now().UTC())
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if !active {
		t.Fatal("expected an active session after handling an inbound message")
	}
}
