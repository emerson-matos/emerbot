package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/emerson/emerbot/apps/webhook/internal/financial"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
	"github.com/emerson/emerbot/packages/whatsapp"
)

func TestFinanceLedgerIgnoresSenderPhone(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	finHandler := financial.NewHandler(whatsapp.NewRegexParser(), store)
	app := New(nil, finHandler, &fakeWhatsAppClient{}, "secret", "verify", wasession.NewInMemoryStore())

	_, status, err := app.Handle(context.Background(), Request{
		UserID:        "phone-A",
		MessageID:     "m1",
		PhoneNumberID: "p1",
		Text:          "/despesa 100 luz",
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	month := time.Now().UTC().Format("2006-01")

	// The entry must land on the shared ledger (R$100 = 10000 centavos)...
	got, err := store.MonthlySummary(context.Background(), shared.FinanceLedgerID, month)
	if err != nil {
		t.Fatalf("summary(ledger): %v", err)
	}
	if got.TotalExpense != 10000 {
		t.Fatalf("expected 10000 centavos on shared ledger, got %d", got.TotalExpense)
	}

	// ...and NOT under the raw phone number.
	byPhone, err := store.MonthlySummary(context.Background(), "phone-A", month)
	if err != nil {
		t.Fatalf("summary(phone): %v", err)
	}
	if byPhone.TotalExpense != 0 {
		t.Fatalf("entry leaked under phone key: got %d", byPhone.TotalExpense)
	}
}

// TestHandleRecordsInboundMessage proves every inbound message opens the
// WhatsApp 24h window: after handling one, the sender's phone has a recorded
// last-inbound timestamp the notifier can later check.
func TestHandleRecordsInboundMessage(t *testing.T) {
	t.Parallel()

	sessions := wasession.NewInMemoryStore()
	// Inbound recording happens before any routing, so /help (which needs
	// neither a financial handler nor the orchestrator service) is enough.
	app := New(nil, nil, &fakeWhatsAppClient{}, "secret", "verify", sessions)

	when := time.Now().UTC().Add(-time.Hour)
	if _, _, err := app.Handle(context.Background(), Request{
		UserID:    "5511999999999",
		MessageID: "m1",
		Text:      "/help",
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
