package finance

import (
	"context"
	"strings"
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

// TestIsRevenue pins that faturamento is decided by Origin alone: category is
// irrelevant, and an income entry with no origin (a stray the backfill never
// reached) is not a sale.
func TestIsRevenue(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    domain.FinancialEntry
		want bool
	}{
		{
			"a sale, whatever its category",
			domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "outros_receitas", Origin: domain.OriginVenda},
			true,
		},
		{
			"a loan is not a sale, whatever its category",
			domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "venda_balcao", Origin: domain.OriginEmprestimo},
			false,
		},
		{
			"an income entry with no origin is a stray, not a sale",
			domain.FinancialEntry{Type: domain.EntryTypeIncome, Category: "venda_balcao"},
			false,
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

// The catalog is the defaults *plus* whatever this user created. Both halves
// matter: a user who has only ever talked to the bot has an empty CAT#
// partition, so validating against the store alone would reject "aluguel" for
// them, and validating against the defaults alone would reject the category
// they asked for one message ago.
func TestCatalogMergesDefaultsAndUserCategories(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	custom, err := domain.NewCategory("u1", "Venda Atacado", "", domain.EntryTypeIncome)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCategory(ctx, custom); err != nil {
		t.Fatal(err)
	}

	cat, err := loadCatalog(ctx, store, "u1")
	if err != nil {
		t.Fatalf("loadCatalog: %v", err)
	}

	if !cat.has("aluguel") {
		t.Error("a default must be usable even when nothing was ever seeded")
	}
	if !cat.has("venda_atacado") {
		t.Error("the user's own category must be usable")
	}
	// A hallucinated category must never reach storage.
	if cat.has("categoria_inventada_pela_llm") || cat.has("") {
		t.Error("catalog accepted a category nobody defined")
	}
	if got := cat.labels()["venda_atacado"]; got != "Venda Atacado" {
		t.Errorf("label = %q, want the label the user gave it", got)
	}
}

// The user's own definition wins on a shared slug: a renamed default is renamed.
func TestCatalogPrefersTheUsersOwnDefinition(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	if err := store.SaveCategory(ctx, domain.Category{
		UserID: "u1", Slug: "venda_balcao", Label: "Balcão (loja 2)", Type: domain.EntryTypeIncome,
	}); err != nil {
		t.Fatal(err)
	}

	cat, err := loadCatalog(ctx, store, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got := cat.labels()["venda_balcao"]; got != "Balcão (loja 2)" {
		t.Errorf("label = %q, want the user's label to win over the default's", got)
	}
}

func TestCatalogValidateRejectsTheWrongType(t *testing.T) {
	cat, err := loadCatalog(context.Background(), NewInMemoryStore(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.validate("aluguel", domain.EntryTypeExpense); err != nil {
		t.Fatalf("validate(aluguel, expense) = %v, want nil", err)
	}
	// An expense category with sales in it is a breakdown that lies later, so it
	// is refused now — and the refusal names the categories that would work.
	err = cat.validate("aluguel", domain.EntryTypeIncome)
	if err == nil {
		t.Fatal("filing income under an expense category must be refused")
	}
	if !strings.Contains(err.Error(), "venda_balcao") {
		t.Errorf("error = %q, want it to list the income categories", err)
	}
	err = cat.validate("nao_existe", domain.EntryTypeExpense)
	if err == nil || !strings.Contains(err.Error(), createCategoryToolName) {
		t.Errorf("error = %v, want it to point at %s", err, createCategoryToolName)
	}
}

// The category names in a breakdown are the user's words. Only where they are
// unreachable does the slug get title-cased — never printed raw.
func TestCategoryLabelFallback(t *testing.T) {
	labels := map[string]string{"venda_atacado": "Venda Atacado"}
	for _, tc := range []struct{ name, slug, want string }{
		{"the user's own label", "venda_atacado", "Venda Atacado"},
		{"a default's label", "aluguel", "Aluguel"},
		{"an unknown slug", "taxa_maquininha", "Taxa Maquininha"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CategoryLabel(labels, tc.slug); got != tc.want {
				t.Errorf("CategoryLabel(%q) = %q, want %q", tc.slug, got, tc.want)
			}
		})
	}
	if got := CategoryLabel(nil, "aluguel"); got != "Aluguel" {
		t.Errorf("CategoryLabel(nil, aluguel) = %q, want the default label", got)
	}
}

// Faturamento is split by category, but never *decided* by it: origin is what
// says a sale is a sale (ADR-016).
func TestRevenueByCategory(t *testing.T) {
	entries := []domain.FinancialEntry{
		{Type: domain.EntryTypeIncome, Origin: domain.OriginVenda, Category: "venda_atacado", Amount: 30000},
		{Type: domain.EntryTypeIncome, Origin: domain.OriginVenda, Category: "venda_atacado", Amount: 10000},
		{Type: domain.EntryTypeIncome, Origin: domain.OriginVenda, Category: "venda_balcao", Amount: 25000},
		// A loan filed under a sales category is still not faturamento.
		{Type: domain.EntryTypeIncome, Origin: domain.OriginEmprestimo, Category: "venda_balcao", Amount: 900000},
		{Type: domain.EntryTypeExpense, Category: "aluguel", Amount: 500000},
	}

	got := RevenueByCategory(entries, map[string]string{"venda_atacado": "Venda Atacado"})

	if len(got) != 2 {
		t.Fatalf("got %d categories (%+v), want atacado and balcão", len(got), got)
	}
	if got[0].Category != "venda_atacado" || got[0].Total != 40000 || got[0].Count != 2 {
		t.Errorf("first = %+v, want venda_atacado at 40000 over 2 entries", got[0])
	}
	if got[0].Label != "Venda Atacado" {
		t.Errorf("Label = %q, want the user's label", got[0].Label)
	}
	if got[1].Category != "venda_balcao" || got[1].Total != 25000 {
		t.Errorf("second = %+v, want venda_balcao at 25000 — the empréstimo excluded", got[1])
	}
}
