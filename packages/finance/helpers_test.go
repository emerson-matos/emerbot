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

// TestRevenueAndCashInDiverge guards the "empréstimo não é faturamento" rule at
// the level the totals see it: a loan is real money in, and must show up in
// entradas de caixa without touching faturamento.
//
// It also pins the second, less obvious direction of the split: an unpaid sale
// is faturamento but is *not* cash. The two figures differ both ways, and a
// change that makes them agree here has broken one of them.
func TestRevenueAndCashInDiverge(t *testing.T) {
	day := date(t, "2026-07-10")
	paid := func(e domain.FinancialEntry) domain.FinancialEntry {
		e.PaymentStatus = domain.PaymentStatusPaid
		e.PaymentDate = &day
		return e
	}

	sale := paid(domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "venda_balcao", Origin: domain.OriginVenda, Amount: 50000})
	convenio := paid(domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "convenio", Origin: domain.OriginVenda, Amount: 30000})
	loan := paid(domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "outros_receitas", Origin: domain.OriginEmprestimo, Amount: 10000000})
	aporte := paid(domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "outros_receitas", Origin: domain.OriginAporteSocio, Amount: 700000})
	unpaidSale := domain.FinancialEntry{
		Type: domain.EntryTypeIncome, Category: "venda_balcao", Origin: domain.OriginVenda,
		Amount: 20000, PaymentStatus: domain.PaymentStatusPending,
	}
	expense := paid(domain.FinancialEntry{Type: domain.EntryTypeExpense, Category: "aluguel", Amount: 5000})

	entries := []domain.FinancialEntry{sale, convenio, loan, aporte, unpaidSale, expense}

	// 50000 + 30000 + 20000 — the unpaid sale counts, the loan and aporte don't.
	if got, want := RevenueTotal(entries), int64(100000); got != want {
		t.Errorf("RevenueTotal = %d, want %d (sales only, paid or not)", got, want)
	}
	// 50000 + 30000 + 10000000 + 700000 — the loan and aporte count, the unpaid
	// sale doesn't.
	if got, want := CashInTotal(entries), int64(10780000); got != want {
		t.Errorf("CashInTotal = %d, want %d (every inflow received, sale or not)", got, want)
	}
}

// TestIsRevenueMigrationShim pins the fallback that keeps faturamento identical
// for entries written before Origin existed. Delete this test in the same
// commit that deletes the shim in domain.IsRevenue — not before.
func TestIsRevenueMigrationShim(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    domain.FinancialEntry
		want bool
	}{
		{
			"legacy sale category, no origin",
			domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "venda_balcao"},
			true,
		},
		{
			"legacy outros_receitas, no origin — the old 'not a sale' marker",
			domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "outros_receitas"},
			false,
		},
		{
			"origin wins over category: a loan filed under venda_balcao",
			domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "venda_balcao", Origin: domain.OriginEmprestimo},
			false,
		},
		{
			"origin wins over category: a sale filed under outros_receitas",
			domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "outros_receitas", Origin: domain.OriginVenda},
			true,
		},
		{
			"an expense is never revenue",
			domain.FinancialEntry{Type: domain.EntryTypeExpense, Category: "aluguel"},
			false,
		},
	} {
		if got := domain.IsRevenue(tc.e); got != tc.want {
			t.Errorf("IsRevenue(%s) = %v, want %v", tc.name, got, tc.want)
		}
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
