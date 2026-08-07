package domain

import "testing"

// The slug is the standard: one form for a category, whoever asked for it. A
// second spelling of "venda atacado" is a second category, and every breakdown
// downstream would then report the pharmacy's wholesale in two halves.
func TestNormalizeCategorySlug(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"already a slug", "venda_atacado", "venda_atacado"},
		{"spaces become underscores", "Venda Atacado", "venda_atacado"},
		{"accents are folded", "Convênio Farmácia", "convenio_farmacia"},
		{"cedilla and tilde", "Promoção São João", "promocao_sao_joao"},
		{"punctuation is dropped", "Venda (atacado) — CNPJ!", "venda_atacado_cnpj"},
		{"hyphens join words", "venda-atacado", "venda_atacado"},
		{"repeats collapse", "venda   ///   atacado", "venda_atacado"},
		{"edges are trimmed", "  _venda atacado_  ", "venda_atacado"},
		{"digits survive", "Convênio 2026", "convenio_2026"},
		{"nothing usable", "!!! ---", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeCategorySlug(tc.in); got != tc.want {
				t.Errorf("NormalizeCategorySlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A slug is a sort-key component, so a pasted paragraph is cut rather than
// stored whole — and cut on a word boundary rather than mid-word plus a
// dangling underscore.
func TestNormalizeCategorySlugIsBounded(t *testing.T) {
	got := NormalizeCategorySlug("Fornecedor de medicamentos genéricos e similares da região metropolitana")
	if len(got) > maxCategorySlugLen {
		t.Fatalf("slug is %d chars (%q), want at most %d", len(got), got, maxCategorySlugLen)
	}
	if got == "" {
		t.Fatal("a long label must still produce a slug")
	}
	if got[len(got)-1] == '_' {
		t.Errorf("slug = %q, want no trailing underscore", got)
	}
}

func TestNewCategory(t *testing.T) {
	cat, err := NewCategory("u1", "  Venda   Atacado  ", "", EntryTypeIncome)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if cat.Slug != "venda_atacado" {
		t.Errorf("Slug = %q, want venda_atacado derived from the label", cat.Slug)
	}
	// The label is what the user typed, accents and all — only the runs of
	// whitespace are collapsed. It is what gets read back to them.
	if cat.Label != "Venda Atacado" {
		t.Errorf("Label = %q, want the label as typed", cat.Label)
	}
	if cat.UserID != "u1" || cat.Type != EntryTypeIncome {
		t.Errorf("category = %+v, want it owned by u1 and typed income", cat)
	}
	// Default means "defined in this codebase", and this one was not. It is
	// still an ordinary category in every other respect — see the type's doc.
	if cat.Default {
		t.Error("a category somebody asked for is not one of the defaults")
	}
}

// A proposed slug is a suggestion, not a key: it goes through the same
// normalization the label does, so no caller can smuggle in a second spelling.
func TestNewCategoryNormalizesAProposedSlug(t *testing.T) {
	cat, err := NewCategory("u1", "Venda Varejo", "Venda-Varejo!", EntryTypeIncome)
	if err != nil {
		t.Fatalf("NewCategory: %v", err)
	}
	if cat.Slug != "venda_varejo" {
		t.Errorf("Slug = %q, want venda_varejo", cat.Slug)
	}
}

func TestNewCategoryRejectsWhatCannotBeStored(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		entryType   EntryType
	}{
		{"empty label", "   ", EntryTypeExpense},
		{"label with nothing sluggable", "!!! ---", EntryTypeExpense},
		{"unknown type", "Venda Atacado", EntryType("outro")},
		{"empty type", "Venda Atacado", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewCategory("u1", tc.label, "", tc.entryType); err == nil {
				t.Fatal("expected an error, got none")
			}
		})
	}
}

// Every default already obeys the standard, which is what makes it the
// standard: a category created today sits beside them without looking different.
func TestDefaultCategoriesFollowTheSlugStandard(t *testing.T) {
	for _, c := range DefaultCategories("u1") {
		if got := NormalizeCategorySlug(c.Slug); got != c.Slug {
			t.Errorf("default %q normalizes to %q — the defaults must be in the same form", c.Slug, got)
		}
	}
}

func TestCategoryLabelFromSlug(t *testing.T) {
	if got := CategoryLabelFromSlug("venda_atacado"); got != "Venda Atacado" {
		t.Errorf("CategoryLabelFromSlug = %q, want %q", got, "Venda Atacado")
	}
	if got := CategoryLabelFromSlug(""); got != "" {
		t.Errorf("CategoryLabelFromSlug(\"\") = %q, want empty", got)
	}
}

func TestCategoryLabels(t *testing.T) {
	labels := CategoryLabels([]Category{
		{Slug: "venda_atacado", Label: "Venda Atacado"},
		{Slug: "convenio", Label: "Convênio Unimed"},
	})
	if labels["convenio"] != "Convênio Unimed" {
		t.Errorf("labels[convenio] = %q, want the user's own label", labels["convenio"])
	}
	if _, ok := labels["aluguel"]; ok {
		t.Error("a slug nobody defined must be absent, so callers can fall back")
	}
}
