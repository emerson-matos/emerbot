package payments

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The storage-key layout has no test coverage from the outside — Save's
// behaviour against real DynamoDB can't run in CI — so these lock down the one
// property the layout has to satisfy: distinct canonical items must map to
// distinct keys. A collision here is not a cosmetic bug. The colliding items
// land in the same BatchWriteItem call, which DynamoDB rejects wholesale
// ("Provided list of item keys contains duplicates"), so a single sale that
// settles twice in a day would fail that day's entire import.

func TestPaySKDistinguishesInstallmentsOnTheSameDay(t *testing.T) {
	date := mustDate(t, "2026-07-23")
	id := NewSaleID(ProviderPagBank, "S1")

	first := paySK(date, id, 1)
	second := paySK(date, id, 2)
	if first == second {
		t.Fatalf("paySK collides for parcelas 1 and 2 on the same day: %q", first)
	}
}

func TestMarshalResultProducesOneKeyPerItem(t *testing.T) {
	repo := &DynamoDBRepository{tableName: "t", ledgerID: "L"}
	date := mustDate(t, "2026-07-23")
	id := NewSaleID(ProviderPagBank, "S1")

	// An anticipated 2x credit sale: both parcelas liquidate on the same day.
	result := ImportResult{
		Provider: ProviderPagBank, SourceDate: date,
		Payments: []Payment{
			{Provider: ProviderPagBank, SaleID: id, PaymentDate: date, Amount: 4900, InstallmentNumber: 1},
			{Provider: ProviderPagBank, SaleID: id, PaymentDate: date, Amount: 4900, InstallmentNumber: 2},
		},
	}
	if err := ValidateImportResult(result); err != nil {
		t.Fatalf("ValidateImportResult: %v", err)
	}

	items, keys, err := repo.marshalResult(result)
	if err != nil {
		t.Fatalf("marshalResult: %v", err)
	}
	if len(items) != len(keys) {
		t.Errorf("%d items collapsed onto %d keys; every item needs its own key", len(items), len(keys))
	}
}

// Two liquidations that really are the same item are a parser bug, and must be
// rejected before a write rather than silently losing one of them.
func TestValidateImportResultRejectsDuplicateKeys(t *testing.T) {
	date := mustDate(t, "2026-07-23")
	id := NewSaleID(ProviderPagBank, "S1")
	pay := Payment{Provider: ProviderPagBank, SaleID: id, PaymentDate: date, Amount: 4900, InstallmentNumber: 1}

	err := ValidateImportResult(ImportResult{
		Provider: ProviderPagBank, SourceDate: date,
		Payments: []Payment{pay, pay},
	})
	if err == nil {
		t.Fatal("expected duplicate payment keys to be rejected")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error = %v, want it to name the duplicate", err)
	}
}

// The import index is what makes Save's replace cheap; its key must be stable
// per (provider, source day) and must not collide with a data item's prefix.
func TestImportSKIsScopedToProviderAndDay(t *testing.T) {
	d1 := mustDate(t, "2026-07-23")
	d2 := mustDate(t, "2026-07-24")

	if a, b := importSK(ProviderPagBank, d1), importSK(ProviderPagBank, d1); a != b {
		t.Errorf("importSK is not stable: %q vs %q", a, b)
	}
	if a, b := importSK(ProviderPagBank, d1), importSK(ProviderPagBank, d2); a == b {
		t.Errorf("importSK collides across source days: %q", a)
	}
	if a, b := importSK(ProviderPagBank, d1), importSK(ProviderStone, d1); a == b {
		t.Errorf("importSK collides across providers: %q", a)
	}
	for _, prefix := range []string{salePrefix, recvPrefix, payPrefix} {
		if strings.HasPrefix(importSK(ProviderPagBank, d1), prefix) {
			t.Errorf("import index key shares the %q data prefix and would be range-queried as data", prefix)
		}
	}
}

// skHigh has to be a valid UTF-8 string that still sorts above every character
// the SKs use: DynamoDB compares strings by their UTF-8 bytes, and a bare 0xFF
// byte is not valid UTF-8.
func TestSKHighIsValidUTF8AndSortsLast(t *testing.T) {
	date := mustDate(t, "2026-07-23")
	upper := salePrefix + date.String() + skHigh

	for _, id := range []string{"pagbank:S1", "pagbank:zzzzzz", "pagbank:~~~", "stone:0"} {
		sk := saleSK(date, SaleID(id))
		if !(sk < upper) {
			t.Errorf("SK %q does not sort below the range upper bound %q", sk, upper)
		}
	}
	if !utf8.ValidString(skHigh) {
		t.Errorf("skHigh %q is not valid UTF-8", skHigh)
	}
}
