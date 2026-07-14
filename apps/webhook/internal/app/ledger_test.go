package app

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/emerson/emerbot/apps/webhook/internal/financial"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/whatsapp"
)

func TestFinanceLedgerIgnoresSenderPhone(t *testing.T) {
	t.Parallel()

	store := pkgfinance.NewInMemoryStore()
	finHandler := financial.NewHandler(whatsapp.NewRegexParser(), store)
	// service can be nil: financial commands short-circuit before it is used.
	app := New(nil, finHandler, &fakeWhatsAppClient{}, "secret", "verify")

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
