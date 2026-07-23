package finance

import (
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func TestItemToEntrySelfHeal(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	baseItem := entryItem{
		PK:          "USER#u1",
		SK:          "ENTRY#2026-07-10#e1",
		GSI1PK:      "USER#u1",
		GSI1SK:      "aluguel#2026-07-10",
		GSI2PK:      "USER#u1",
		GSI2SK:      "2026-07-10#e1",
		EntryID:     "e1",
		UserID:      "u1",
		Date:        "2026-07-10",
		Amount:      50000,
		Category:    "aluguel",
		Type:        "expense",
		Description: "Aluguel",
		Source:      "manual",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	t.Run("pending with payment date is self-healed", func(t *testing.T) {
		item := baseItem
		item.PaymentStatus = "pending"
		item.PaymentDate = "2026-07-12" // bad data: pending should not have payment date

		e, err := itemToEntry(item)
		if err != nil {
			t.Fatalf("itemToEntry: %v", err)
		}
		if e.PaymentDate != nil {
			t.Fatal("expected PaymentDate to be nil after self-heal")
		}
	})

	t.Run("paid without payment date is self-healed", func(t *testing.T) {
		item := baseItem
		item.PaymentStatus = "paid"
		item.PaymentDate = "" // bad data: paid should have payment date

		e, err := itemToEntry(item)
		if err != nil {
			t.Fatalf("itemToEntry: %v", err)
		}
		if e.PaymentDate == nil {
			t.Fatal("expected PaymentDate to be set after self-heal")
		}
		want := domain.NewCalendarDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
		if !e.PaymentDate.Equal(want) {
			t.Fatalf("PaymentDate = %v, want %v", *e.PaymentDate, want)
		}
	})

	t.Run("valid paid entry unchanged", func(t *testing.T) {
		item := baseItem
		item.PaymentStatus = "paid"
		item.PaymentDate = "2026-07-12"

		e, err := itemToEntry(item)
		if err != nil {
			t.Fatalf("itemToEntry: %v", err)
		}
		if e.PaymentDate == nil {
			t.Fatal("expected PaymentDate to be set")
		}
		want := domain.NewCalendarDate(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
		if !e.PaymentDate.Equal(want) {
			t.Fatalf("PaymentDate = %v, want %v", *e.PaymentDate, want)
		}
	})

	t.Run("valid pending entry unchanged", func(t *testing.T) {
		item := baseItem
		item.PaymentStatus = "pending"
		item.PaymentDate = ""

		e, err := itemToEntry(item)
		if err != nil {
			t.Fatalf("itemToEntry: %v", err)
		}
		if e.PaymentDate != nil {
			t.Fatal("expected PaymentDate to be nil")
		}
	})
}
