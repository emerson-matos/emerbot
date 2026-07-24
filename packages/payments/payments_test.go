package payments

import (
	"context"
	"errors"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

func mustDate(t *testing.T, s string) domain.CalendarDate {
	t.Helper()
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func TestParseCentavos(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"108.96", 10896},
		{"107.5", 10750},
		{"100", 10000},
		{"", 0},
		{"0.01", 1},
		{"1.005", 101}, // third digit rounds half-up
		{"1.004", 100},
		{"-5.00", -500},
	}
	for _, c := range cases {
		got, err := ParseCentavos(c.in)
		if err != nil {
			t.Errorf("ParseCentavos(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseCentavos(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// sampleResult builds one logical import with a sale and two receivables (one
// far in the future) plus a payment.
func sampleResult(t *testing.T, receivables ...ExpectedReceivable) ImportResult {
	t.Helper()
	saleID := NewSaleID(ProviderPagBank, "S1")
	return ImportResult{
		Provider:   ProviderPagBank,
		SourceDate: mustDate(t, "2026-07-23"),
		Sales: []Sale{{
			ID: saleID, Provider: ProviderPagBank, ExternalID: "S1",
			SaleDate: mustDate(t, "2026-07-23"), GrossAmount: 10000, NetAmount: 9800, FeeAmount: 200,
			Method: MethodCredito, Brand: "VISA", Installments: 2,
		}},
		Receivables: receivables,
		Payments: []Payment{{
			Provider: ProviderPagBank, SaleID: saleID, PaymentDate: mustDate(t, "2026-07-23"), Amount: 9800,
		}},
	}
}

func TestInMemoryRepositoryReplaceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryRepository()
	saleID := NewSaleID(ProviderPagBank, "S1")
	r1 := ExpectedReceivable{Provider: ProviderPagBank, SaleID: saleID, ExpectedDate: mustDate(t, "2026-08-23"), Amount: 4900, InstallmentNumber: 1, InstallmentTotal: 2}
	r2 := ExpectedReceivable{Provider: ProviderPagBank, SaleID: saleID, ExpectedDate: mustDate(t, "2026-09-23"), Amount: 4900, InstallmentNumber: 2, InstallmentTotal: 2}

	full := sampleResult(t, r1, r2)
	if err := repo.Save(ctx, full); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	// Re-import the identical envelope: state must be unchanged (idempotent).
	if err := repo.Save(ctx, full); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	broad := func() []ExpectedReceivable {
		got, err := repo.ListReceivables(ctx, mustDate(t, "2000-01-01"), mustDate(t, "2100-01-01"))
		if err != nil {
			t.Fatalf("ListReceivables: %v", err)
		}
		return got
	}
	if len(broad()) != 2 {
		t.Fatalf("after re-import, receivables = %d, want 2", len(broad()))
	}
	sales, _ := repo.ListSales(ctx, mustDate(t, "2000-01-01"), mustDate(t, "2100-01-01"))
	if len(sales) != 1 {
		t.Fatalf("sales = %d, want 1 (no duplication)", len(sales))
	}

	// Correction: re-import the same (provider, source day) dropping the future
	// receivable r2. The replace must remove it even though its date is far off.
	if err := repo.Save(ctx, sampleResult(t, r1)); err != nil {
		t.Fatalf("Save correction: %v", err)
	}
	got := broad()
	if len(got) != 1 || !got[0].ExpectedDate.Equal(r1.ExpectedDate) {
		t.Fatalf("after correction, receivables = %+v, want only r1", got)
	}
}

func TestValidateImportResultRejectsMixedProvider(t *testing.T) {
	r := sampleResult(t)
	r.Receivables = []ExpectedReceivable{{
		Provider: ProviderStone, SaleID: NewSaleID(ProviderStone, "X"),
		ExpectedDate: mustDate(t, "2026-08-01"), Amount: 100, InstallmentNumber: 1, InstallmentTotal: 1,
	}}
	if err := ValidateImportResult(r); err == nil {
		t.Error("expected mixed-provider ImportResult to be rejected")
	}
}

func TestValidateImportResultRejectsMissingSourceDate(t *testing.T) {
	r := sampleResult(t)
	r.SourceDate = domain.CalendarDate{}
	if err := ValidateImportResult(r); err == nil {
		t.Error("expected missing source date to be rejected")
	}
}

// fakeParser lets us drive ImportService without a real provider.
type fakeParser struct{ result ImportResult }

func (f fakeParser) Parse([]byte) (ImportResult, error) { return f.result, nil }

// recordingRepo records whether Save was called.
type recordingRepo struct {
	InMemoryRepository
	saved bool
}

func (r *recordingRepo) Save(ctx context.Context, result ImportResult) error {
	r.saved = true
	return r.InMemoryRepository.Save(ctx, result)
}

func TestImportServiceRejectsProviderMismatch(t *testing.T) {
	// Parser (reached via the pagbank route) returns a Stone-labelled result.
	repo := &recordingRepo{InMemoryRepository: *NewInMemoryRepository()}
	svc := NewImportService(
		map[Provider]Parser{ProviderPagBank: fakeParser{result: ImportResult{Provider: ProviderStone, SourceDate: mustDate(t, "2026-07-23")}}},
		repo,
	)
	err := svc.Process(context.Background(), ProviderPagBank, []byte(`{}`))
	if !errors.Is(err, ErrProviderMismatch) {
		t.Fatalf("err = %v, want ErrProviderMismatch", err)
	}
	if repo.saved {
		t.Error("Save must not run when the provider mismatches")
	}
}

func TestImportServiceUnknownProvider(t *testing.T) {
	svc := NewImportService(map[Provider]Parser{}, NewInMemoryRepository())
	err := svc.Process(context.Background(), ProviderStone, []byte(`{}`))
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("err = %v, want ErrUnknownProvider", err)
	}
}
