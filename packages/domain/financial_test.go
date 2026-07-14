package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFinancialEntryMarshalJSONEncodesDatesAsCalendarDates(t *testing.T) {
	t.Parallel()

	due := time.Date(2026, 7, 20, 15, 30, 0, 0, time.UTC)
	entry := FinancialEntry{
		UserID:    "u1",
		EntryID:   "e1",
		Date:      time.Date(2026, 7, 10, 23, 59, 0, 0, time.UTC),
		Amount:    50000,
		Category:  "aluguel",
		Type:      EntryTypeExpense,
		DueDate:   &due,
		CreatedAt: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}

	if raw["Date"] != "2026-07-10" {
		t.Fatalf("expected Date %q, got %v", "2026-07-10", raw["Date"])
	}
	if raw["DueDate"] != "2026-07-20" {
		t.Fatalf("expected DueDate %q, got %v", "2026-07-20", raw["DueDate"])
	}
	if _, ok := raw["PaymentDate"]; ok {
		t.Fatalf("expected PaymentDate to be omitted when nil, got %v", raw["PaymentDate"])
	}

	// No timestamp/timezone leakage in the date-only fields.
	if strings.Contains(raw["Date"].(string), "T") || strings.Contains(raw["DueDate"].(string), "T") {
		t.Fatalf("expected date-only strings with no time component, got Date=%v DueDate=%v", raw["Date"], raw["DueDate"])
	}

	// CreatedAt/UpdatedAt remain full instants.
	if createdAt, _ := raw["CreatedAt"].(string); !strings.Contains(createdAt, "T") {
		t.Fatalf("expected CreatedAt to keep its RFC3339 instant, got %v", raw["CreatedAt"])
	}
}

func TestFinancialEntryJSONRoundTripsCalendarDates(t *testing.T) {
	t.Parallel()

	due := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	original := FinancialEntry{
		UserID:   "u1",
		EntryID:  "e1",
		Date:     time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		Amount:   50000,
		Category: "aluguel",
		Type:     EntryTypeExpense,
		DueDate:  &due,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded FinancialEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if !decoded.Date.Equal(original.Date) {
		t.Fatalf("expected Date %v, got %v", original.Date, decoded.Date)
	}
	if decoded.DueDate == nil || !decoded.DueDate.Equal(*original.DueDate) {
		t.Fatalf("expected DueDate %v, got %v", *original.DueDate, decoded.DueDate)
	}
	if decoded.PaymentDate != nil {
		t.Fatalf("expected nil PaymentDate, got %v", decoded.PaymentDate)
	}
}
