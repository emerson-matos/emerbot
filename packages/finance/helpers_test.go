package finance

import (
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

func TestParseDate(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
	}{
		{"2026-07-20", true},
		{"", false},
		{"20/07/2026", false},
		{"2026-13-01", false}, // month 13
		{"hoje", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseDate(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("parseDate(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if ok && got.Format("2006-01-02") != tc.in {
				t.Fatalf("parseDate(%q) = %v, want the same day back", tc.in, got)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	// Tool args come from LLM output, so an absent, negative or absurd limit
	// must land on a sane bound rather than reaching the store as-is.
	cases := map[int]int{
		0:    defaultEntryLimit, // unset
		-5:   defaultEntryLimit, // nonsense
		1:    1,                 //
		50:   50,                //
		200:  maxEntryLimit,     // exactly the cap
		201:  maxEntryLimit,     // over the cap
		9999: maxEntryLimit,
	}
	for in, want := range cases {
		if got := clampLimit(in); got != want {
			t.Fatalf("clampLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestReaisToCentavosRoundsRatherThanTruncating(t *testing.T) {
	cases := map[float64]int64{
		19.99:  1999, // float truncation would give 1998
		0.1:    10,
		0:      0,
		1:      100,
		0.005:  1, // rounds up at the half centavo
		0.004:  0, // and down below it
		-19.99: -1999,
		-0.1:   -10,
	}
	for in, want := range cases {
		if got := reaisToCentavos(in); got != want {
			t.Fatalf("reaisToCentavos(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestCentavosToReaisRoundTrip(t *testing.T) {
	for _, centavos := range []int64{0, 1, 99, 100, 1999, 1234567} {
		if got := reaisToCentavos(centavosToReais(centavos)); got != centavos {
			t.Fatalf("round trip of %d centavos gave %d", centavos, got)
		}
	}
}

// TestIsFaturamentoExcludesOutrosReceitas guards the "empréstimo não é
// faturamento" fix: a loan, aporte or investment redemption logged under the
// "Outros (Receita)" catch-all must not count as faturamento, even though it
// is real income (IncomeTotal), because a goal is "quanto vendemos".
func TestIsFaturamentoExcludesOutrosReceitas(t *testing.T) {
	sale := domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "venda_balcao", Amount: 50000}
	convenio := domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "convenio", Amount: 30000}
	delivery := domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "delivery", Amount: 20000}
	loan := domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "outros_receitas", Amount: 10000000}
	expense := domain.FinancialEntry{Type: domain.EntryTypeExpense, Category: "aluguel", Amount: 5000}

	for _, tc := range []struct {
		name string
		e    domain.FinancialEntry
		want bool
	}{
		{"counter sale", sale, true},
		{"convenio", convenio, true},
		{"delivery", delivery, true},
		{"outros_receitas (a loan filed here)", loan, false},
		{"expense", expense, false},
	} {
		if got := IsFaturamento(tc.e); got != tc.want {
			t.Errorf("IsFaturamento(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}

	entries := []domain.FinancialEntry{sale, convenio, delivery, loan, expense}
	if got := FaturamentoTotal(entries); got != 100000 {
		t.Errorf("FaturamentoTotal = %d, want 100000 (sale+convenio+delivery, loan excluded)", got)
	}
	if got := IncomeTotal(entries); got != 10100000 {
		t.Errorf("IncomeTotal = %d, want 10100000 (every income entry, loan included)", got)
	}
}

func TestKnownCategory(t *testing.T) {
	slugs := categorySlugs()
	if len(slugs) == 0 {
		t.Fatal("expected the domain to define default categories")
	}
	if !knownCategory(slugs[0]) {
		t.Fatalf("knownCategory(%q) = false, want true for a default slug", slugs[0])
	}
	// A hallucinated category must be rejected so it never reaches storage.
	if knownCategory("categoria_inventada_pela_llm") {
		t.Fatal("knownCategory accepted a category that is not defined")
	}
	if knownCategory("") {
		t.Fatal("knownCategory accepted an empty category")
	}
}
