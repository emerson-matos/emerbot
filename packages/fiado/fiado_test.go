package fiado

import (
	"errors"
	"strings"
	"testing"

	"github.com/emerson/emerbot/packages/domain"
)

func TestNewMovementNormalizesTheClient(t *testing.T) {
	m, err := NewMovement(user, "  João   Silva ", 4000, day(t, "2026-08-10"), " levou dipirona ")
	if err != nil {
		t.Fatalf("new movement: %v", err)
	}
	if m.Client != "joao_silva" {
		t.Fatalf("slug = %q, want joao_silva", m.Client)
	}
	// The name is kept as typed (whitespace collapsed), because that is what
	// the screen shows and what the bot says back.
	if m.Name != "João Silva" {
		t.Fatalf("nome = %q, want %q", m.Name, "João Silva")
	}
	if m.Description != "levou dipirona" {
		t.Fatalf("descrição = %q, want %q", m.Description, "levou dipirona")
	}
	if m.ID == "" {
		t.Fatal("movimento sem ULID: dois movimentos do mesmo dia se sobrescreveriam")
	}
}

// The slug is domain.NormalizeCategorySlug and nothing else: one slug form in
// the system, or the same person becomes two debtors.
func TestClientSlugIsTheCategorySlug(t *testing.T) {
	for _, name := range []string{"João Silva", "JOÃO  SILVA", "joão silva"} {
		if got, want := ClientSlug(name), domain.NormalizeCategorySlug(name); got != want {
			t.Fatalf("ClientSlug(%q) = %q, want %q", name, got, want)
		}
	}
	if got := ClientSlug("João Silva"); got != "joao_silva" {
		t.Fatalf("slug = %q, want joao_silva", got)
	}
}

func TestNewMovementRefusesWhatCannotBeRecorded(t *testing.T) {
	cases := map[string]struct {
		name   string
		amount int64
		want   error
	}{
		// "Fiado de quem?" is a one-word question, and an anonymous debt is
		// unrecoverable.
		"sem cliente":       {name: "", amount: 4000, want: ErrNoClient},
		"cliente só emoji":  {name: "🙂", amount: 4000, want: ErrNoClient},
		"valor zero":        {name: "João", amount: 0, want: ErrZeroAmount},
		"valor estratosf.":  {name: "João", amount: MaxAmountCentavos + 1, want: ErrAmountTooLarge},
		"pagamento absurdo": {name: "João", amount: -MaxAmountCentavos - 1, want: ErrAmountTooLarge},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			_, err := NewMovement(user, tc.name, tc.amount, day(t, "2026-08-10"), "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewMovementBoundsTheFreeText(t *testing.T) {
	if _, err := NewMovement(user, strings.Repeat("a", MaxNameLen+1), 4000, day(t, "2026-08-10"), ""); err == nil {
		t.Fatal("nome longo demais foi aceito")
	}
	if _, err := NewMovement(user, "João", 4000, day(t, "2026-08-10"), strings.Repeat("a", MaxDescriptionLen+1)); err == nil {
		t.Fatal("descrição longa demais foi aceita")
	}
}

func TestDaysOpen(t *testing.T) {
	since := day(t, "2026-08-01")
	cases := map[string]struct {
		debtor Debtor
		today  string
		want   string // "" means nil
	}{
		"em aberto há 9 dias":          {Debtor{Balance: 4000, Since: &since}, "2026-08-10", "9"},
		"dívida de hoje não tem idade": {Debtor{Balance: 4000, Since: &since}, "2026-08-01", ""},
		"conta quitada":                {Debtor{Balance: 0, Since: &since}, "2026-08-10", ""},
		"crédito do cliente":           {Debtor{Balance: -500, Since: &since}, "2026-08-10", ""},
		"sem desde":                    {Debtor{Balance: 4000}, "2026-08-10", ""},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			got := DaysOpen(tc.debtor, day(t, tc.today))
			switch {
			case tc.want == "" && got != nil:
				t.Fatalf("dias = %d, want nenhum", *got)
			case tc.want != "" && got == nil:
				t.Fatalf("dias = nenhum, want %s", tc.want)
			case tc.want != "" && *got != 9:
				t.Fatalf("dias = %d, want 9", *got)
			}
		})
	}
}

func TestTotalsSplitByTheSign(t *testing.T) {
	movements := []Movement{
		{Amount: 4000},
		{Amount: -1500},
		{Amount: 2500},
		{Amount: -1000},
	}
	taken, paid := Totals(movements)
	if taken != 6500 || paid != 2500 {
		t.Fatalf("comprado/pago = %d/%d, want 6500/2500", taken, paid)
	}
	if got := Sum(movements); got != 4000 {
		t.Fatalf("saldo = %d, want 4000", got)
	}
}

func TestSinceFromMovements(t *testing.T) {
	cases := map[string]struct {
		movements []Movement
		want      string
	}{
		"uma compra": {
			movements: []Movement{{Date: day(t, "2026-08-01"), Amount: 4000}},
			want:      "2026-08-01",
		},
		// The debt is unbroken from the first purchase, not from the last one.
		"duas compras": {
			movements: []Movement{
				{Date: day(t, "2026-08-01"), Amount: 4000},
				{Date: day(t, "2026-08-05"), Amount: 1000},
			},
			want: "2026-08-01",
		},
		"quitado": {
			movements: []Movement{
				{Date: day(t, "2026-08-01"), Amount: 4000},
				{Date: day(t, "2026-08-05"), Amount: -4000},
			},
			want: "",
		},
		// Settling and buying again is exactly where "desde" and "a compra mais
		// antiga em aberto" part ways.
		"quitou e voltou a comprar": {
			movements: []Movement{
				{Date: day(t, "2026-08-01"), Amount: 4000},
				{Date: day(t, "2026-08-05"), Amount: -4000},
				{Date: day(t, "2026-08-20"), Amount: 900},
			},
			want: "2026-08-20",
		},
		"crédito não é dívida": {
			movements: []Movement{
				{Date: day(t, "2026-08-01"), Amount: 4000},
				{Date: day(t, "2026-08-05"), Amount: -5000},
			},
			want: "",
		},
		"crédito consumido por uma compra": {
			movements: []Movement{
				{Date: day(t, "2026-08-01"), Amount: 4000},
				{Date: day(t, "2026-08-05"), Amount: -5000},
				{Date: day(t, "2026-08-09"), Amount: 3000},
			},
			want: "2026-08-09",
		},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			got := sinceFromMovements(tc.movements)
			switch {
			case tc.want == "" && got != nil:
				t.Fatalf("desde = %q, want nenhum", got.String())
			case tc.want != "" && got == nil:
				t.Fatalf("desde = nenhum, want %s", tc.want)
			case tc.want != "" && got.String() != tc.want:
				t.Fatalf("desde = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

// Without this the caderninho lies for less: "joão", "João Silva" and "Joao S."
// become three debtors, and "quanto o João me deve" answers with a third of it.
func TestSimilarClients(t *testing.T) {
	book := []Debtor{
		{Client: "joao_silva", Balance: 30000},
		{Client: "silva_maria", Balance: 1000},
		{Client: "ana", Balance: 500},
	}
	cases := map[string]struct {
		slug string
		want []string
	}{
		"primeiro nome igual":      {"joao", []string{"joao_silva"}},
		"abreviação do sobrenome":  {"joao_s", []string{"joao_silva"}},
		"prefixo do primeiro nome": {"joa", []string{"joao_silva"}},
		// Two different people who happen to share the leading token are two
		// people. Asking "é o Silva Maria?" for a Silva João is noise, and a
		// tool that asks about everything gets ignored.
		"token compartilhado não basta": {"silva_joao", nil},
		"ninguém parecido":              {"pedro", nil},
		"o próprio não é candidato":     {"ana", nil},
		"vazio":                         {"", nil},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			got := SimilarClients(tc.slug, book)
			names := make([]string, 0, len(got))
			for _, d := range got {
				names = append(names, d.Client)
			}
			if strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("candidatos = %v, want %v", names, tc.want)
			}
		})
	}
}

// A single letter must not match half the book: a tool that asks about
// everything is as useless as one that asks about nothing.
func TestSimilarClientsIgnoresVeryShortPrefixes(t *testing.T) {
	book := []Debtor{{Client: "joao_silva"}, {Client: "jose"}}
	if got := SimilarClients("j", book); len(got) != 0 {
		t.Fatalf("candidatos para %q = %+v, want nenhum", "j", got)
	}
}

func TestClampLimit(t *testing.T) {
	for in, want := range map[int]int{0: DefaultPageLimit, -1: DefaultPageLimit, 10: 10, MaxPageLimit + 1: MaxPageLimit} {
		if got := ClampLimit(in); got != want {
			t.Fatalf("ClampLimit(%d) = %d, want %d", in, got, want)
		}
	}
}
