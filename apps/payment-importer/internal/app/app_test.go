package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/payments"
)

func mustDate(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return d
}

// TestProcessRawImportsEnvelopeFile drives the whole importer pipeline —
// ReadMetadata → parser → repository — from a combined envelope file (a small
// mock built from PagBank's official debit scenario) and asserts what was
// persisted, so a layout change surfaces here, not only in production.
func TestProcessRawImportsEnvelopeFile(t *testing.T) {
	ctx := context.Background()
	raw, err := os.ReadFile(filepath.Join("testdata", "pagbank-combined.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	repo := payments.NewInMemoryRepository()
	if err := New(repo).ProcessRaw(ctx, raw); err != nil {
		t.Fatalf("ProcessRaw: %v", err)
	}

	from, to := mustDate(t, "2000-01-01"), mustDate(t, "2100-01-01")

	sales, err := repo.ListSales(ctx, from, to)
	if err != nil {
		t.Fatalf("ListSales: %v", err)
	}
	if len(sales) != 1 {
		t.Fatalf("sales = %d, want 1", len(sales))
	}
	s := sales[0]
	if s.GrossAmount != 10000 || s.NetAmount != 9845 || s.FeeAmount != 155 {
		t.Errorf("sale amounts gross/net/fee = %d/%d/%d, want 10000/9845/155", s.GrossAmount, s.NetAmount, s.FeeAmount)
	}
	if s.Method != payments.MethodDebito || s.Provider != payments.ProviderPagBank {
		t.Errorf("sale method/provider = %q/%q", s.Method, s.Provider)
	}

	recv, err := repo.ListReceivables(ctx, from, to)
	if err != nil {
		t.Fatalf("ListReceivables: %v", err)
	}
	if len(recv) != 1 || recv[0].Amount != 9845 {
		t.Errorf("receivables = %+v", recv)
	}

	pays, err := repo.ListPayments(ctx, from, to)
	if err != nil {
		t.Fatalf("ListPayments: %v", err)
	}
	if len(pays) != 1 || pays[0].Amount != 9845 {
		t.Errorf("payments = %+v", pays)
	}
}

// TestProcessRawRejectsUnknownProvider ensures a bad envelope fails cleanly and
// persists nothing.
func TestProcessRawRejectsUnknownProvider(t *testing.T) {
	repo := payments.NewInMemoryRepository()
	err := New(repo).ProcessRaw(context.Background(), []byte(`{"provider":"cielo","date":"2024-07-29"}`))
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	sales, _ := repo.ListSales(context.Background(), mustDate(t, "2000-01-01"), mustDate(t, "2100-01-01"))
	if len(sales) != 0 {
		t.Errorf("nothing should be persisted, got %d sales", len(sales))
	}
}
