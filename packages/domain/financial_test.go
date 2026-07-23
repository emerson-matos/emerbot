package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCalendarDateNormalizesAndParses(t *testing.T) {
	d := NewCalendarDate(time.Date(2026, 7, 10, 23, 59, 0, 0, time.FixedZone("x", -3*3600)))
	if got := d.String(); got != "2026-07-10" {
		t.Fatalf("got %s", got)
	}
	if _, err := ParseCalendarDate("2026-99-99"); err == nil {
		t.Fatal("expected invalid date")
	}
}

func TestCalendarDateJSON(t *testing.T) {
	d := NewCalendarDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))

	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(b); got != `"2026-07-10"` {
		t.Fatalf("MarshalJSON = %s, want %q", got, `"2026-07-10"`)
	}

	var d2 CalendarDate
	if err := json.Unmarshal(b, &d2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !d.Equal(d2) {
		t.Fatalf("round-trip: %v != %v", d, d2)
	}

	if err := json.Unmarshal([]byte(`"abc"`), &d2); err == nil {
		t.Fatal("expected error for invalid date string")
	}
}

func TestCalendarDateComparisonGetters(t *testing.T) {
	d1 := NewCalendarDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
	d2 := NewCalendarDate(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC))

	if !d1.Before(d2) {
		t.Error("d1 should be before d2")
	}
	if d1.After(d2) {
		t.Error("d1 should not be after d2")
	}
	if !d1.Equal(d1) {
		t.Error("d1 should equal itself")
	}
	if d1.Equal(d2) {
		t.Error("d1 should not equal d2")
	}
	if d1.Year() != 2026 {
		t.Errorf("Year = %d, want 2026", d1.Year())
	}
	if d1.Month() != time.July {
		t.Errorf("Month = %v, want July", d1.Month())
	}
	if d1.Day() != 10 {
		t.Errorf("Day = %d, want 10", d1.Day())
	}
	if loc := d1.UTC().Location(); loc != time.UTC {
		t.Errorf("UTC location = %v, want UTC", loc)
	}
	if d1.Format("2006") != "2026" {
		t.Errorf("Format = %q, want %q", d1.Format("2006"), "2026")
	}
}

func TestNormalizeSource(t *testing.T) {
	tests := []struct {
		input string
		want  EntrySource
	}{
		{"whatsapp", SourceWhatsApp},
		{"manual", SourceManual},
		{"unknown", SourceUnknown},
		{"seed", SourceUnknown},
		{"", SourceUnknown},
		{"whatever", SourceUnknown},
	}
	for _, tt := range tests {
		if got := NormalizeSource(tt.input); got != tt.want {
			t.Errorf("NormalizeSource(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func validEntry() FinancialEntry {
	d := NewCalendarDate(time.Now())
	e, err := NewFinancialEntry(NewFinancialEntryInput{
		UserID: "u", TransactionDate: d, Amount: 100,
		Type: EntryTypeExpense, PaymentStatus: PaymentStatusPaid, Source: SourceManual,
	})
	if err != nil {
		panic(err)
	}
	return e
}

func TestFinancialEntryValidate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		if err := validEntry().Validate(); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("missing user id", func(t *testing.T) {
		e := validEntry()
		e.UserID = ""
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing entry id", func(t *testing.T) {
		e := validEntry()
		e.EntryID = ""
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("amount zero", func(t *testing.T) {
		e := validEntry()
		e.Amount = 0
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("amount negative", func(t *testing.T) {
		e := validEntry()
		e.Amount = -1
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid transaction date", func(t *testing.T) {
		e := validEntry()
		e.TransactionDate = CalendarDate{}
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid type", func(t *testing.T) {
		e := validEntry()
		e.Type = "invalid"
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid source", func(t *testing.T) {
		e := validEntry()
		e.Source = "hack"
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid payment status", func(t *testing.T) {
		e := validEntry()
		e.PaymentStatus = "unknown"
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("paid missing payment date", func(t *testing.T) {
		e := validEntry()
		e.PaymentDate = nil
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("pending cannot have payment date", func(t *testing.T) {
		e := validEntry()
		e.PaymentStatus = PaymentStatusPending
		if err := e.Validate(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestNewFinancialEntryDefaults(t *testing.T) {
	d := NewCalendarDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))

	t.Run("paid without payment date defaults to transaction date", func(t *testing.T) {
		e, err := NewFinancialEntry(NewFinancialEntryInput{
			UserID: "u", TransactionDate: d, Amount: 100,
			Type: EntryTypeExpense, PaymentStatus: PaymentStatusPaid, Source: SourceManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		if e.PaymentDate == nil {
			t.Fatal("expected PaymentDate to be set")
		}
		if !e.PaymentDate.Equal(d) {
			t.Fatalf("PaymentDate = %v, want %v", *e.PaymentDate, d)
		}
	})

	t.Run("paid with explicit payment date preserved", func(t *testing.T) {
		payDate := NewCalendarDate(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
		e, err := NewFinancialEntry(NewFinancialEntryInput{
			UserID: "u", TransactionDate: d, Amount: 100,
			Type: EntryTypeExpense, PaymentStatus: PaymentStatusPaid,
			PaymentDate: &payDate, Source: SourceManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		if e.PaymentDate == nil || !e.PaymentDate.Equal(payDate) {
			t.Fatalf("PaymentDate = %v, want %v", e.PaymentDate, payDate)
		}
	})

	t.Run("pending strips payment date", func(t *testing.T) {
		payDate := d
		e, err := NewFinancialEntry(NewFinancialEntryInput{
			UserID: "u", TransactionDate: d, Amount: 100,
			Type: EntryTypeExpense, PaymentStatus: PaymentStatusPending,
			PaymentDate: &payDate, Source: SourceManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		if e.PaymentDate != nil {
			t.Fatal("expected PaymentDate to be nil")
		}
	})

	t.Run("pending without payment date stays nil", func(t *testing.T) {
		e, err := NewFinancialEntry(NewFinancialEntryInput{
			UserID: "u", TransactionDate: d, Amount: 100,
			Type: EntryTypeExpense, PaymentStatus: PaymentStatusPending, Source: SourceManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		if e.PaymentDate != nil {
			t.Fatal("expected PaymentDate to be nil")
		}
	})

	t.Run("generates entry id", func(t *testing.T) {
		e, err := NewFinancialEntry(NewFinancialEntryInput{
			UserID: "u", TransactionDate: d, Amount: 100,
			Type: EntryTypeExpense, PaymentStatus: PaymentStatusPaid, Source: SourceManual,
		})
		if err != nil {
			t.Fatal(err)
		}
		if e.EntryID == "" {
			t.Fatal("expected EntryID to be generated")
		}
	})
}

func TestAmountReais(t *testing.T) {
	tests := []struct {
		amount int64
		want   string
	}{
		{0, "0,00"},
		{1, "0,01"},
		{10, "0,10"},
		{100, "1,00"},
		{101, "1,01"},
		{1500, "15,00"},
		{99999, "999,99"},
		{100000, "1000,00"},
		{123456, "1234,56"},
	}
	for _, tt := range tests {
		e := FinancialEntry{Amount: tt.amount}
		if got := e.AmountReais(); got != tt.want {
			t.Errorf("AmountReais(%d) = %q, want %q", tt.amount, got, tt.want)
		}
	}
}

func TestDefaultCategories(t *testing.T) {
	cats := DefaultCategories("u1")
	if len(cats) != 15 {
		t.Fatalf("got %d categories, want 15", len(cats))
	}
	for _, c := range cats {
		if c.UserID != "u1" {
			t.Errorf("UserID = %q, want %q", c.UserID, "u1")
		}
		if !c.Default {
			t.Error("all default categories should have Default=true")
		}
	}
	if cats[0].Type != EntryTypeExpense {
		t.Errorf("first category type = %v, want expense", cats[0].Type)
	}
	last := cats[len(cats)-1]
	if last.Type != EntryTypeIncome {
		t.Errorf("last category type = %v, want income", last.Type)
	}
	if last.Slug != "outros_receitas" {
		t.Errorf("last slug = %q, want %q", last.Slug, "outros_receitas")
	}
}

func TestMemoryKey(t *testing.T) {
	m := Memory{Type: "session", ID: "abc"}
	if got := m.Key(); got != "session#abc" {
		t.Errorf("Key() = %q, want %q", got, "session#abc")
	}
}

func TestDefaultNotificationPrefs(t *testing.T) {
	p := DefaultNotificationPrefs("u1")
	if p.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", p.UserID, "u1")
	}
	if p.WAEnabled {
		t.Error("WAEnabled should be false")
	}
	if !p.NotifyDueToday {
		t.Error("NotifyDueToday should be true")
	}
	if !p.NotifyOverdue {
		t.Error("NotifyOverdue should be true")
	}
	if p.NotifyGoal {
		t.Error("NotifyGoal should be false")
	}
	if p.Phone != "" {
		t.Errorf("Phone = %q, want empty", p.Phone)
	}
}

func TestNormalize(t *testing.T) {
	d := NewCalendarDate(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))
	payDate := NewCalendarDate(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))

	t.Run("pending strips payment date", func(t *testing.T) {
		e := FinancialEntry{
			PaymentStatus: PaymentStatusPending,
			PaymentDate:   &payDate,
		}
		e.Normalize()
		if e.PaymentDate != nil {
			t.Fatal("expected PaymentDate to be nil")
		}
	})

	t.Run("paid without payment date defaults to transaction date", func(t *testing.T) {
		e := FinancialEntry{
			TransactionDate: d,
			PaymentStatus:   PaymentStatusPaid,
		}
		e.Normalize()
		if e.PaymentDate == nil {
			t.Fatal("expected PaymentDate to be set")
		}
		if !e.PaymentDate.Equal(d) {
			t.Fatalf("PaymentDate = %v, want %v", *e.PaymentDate, d)
		}
	})

	t.Run("paid with explicit payment date preserved", func(t *testing.T) {
		e := FinancialEntry{
			TransactionDate: d,
			PaymentStatus:   PaymentStatusPaid,
			PaymentDate:     &payDate,
		}
		e.Normalize()
		if e.PaymentDate == nil || !e.PaymentDate.Equal(payDate) {
			t.Fatalf("PaymentDate = %v, want %v", e.PaymentDate, payDate)
		}
	})

	t.Run("pending without payment date stays nil", func(t *testing.T) {
		e := FinancialEntry{
			PaymentStatus: PaymentStatusPending,
		}
		e.Normalize()
		if e.PaymentDate != nil {
			t.Fatal("expected PaymentDate to be nil")
		}
	})
}
