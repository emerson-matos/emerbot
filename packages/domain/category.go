package domain

import (
	"errors"
	"strings"
	"unicode"
)

// Category represents a financial category for organizing entries.
//
// There is one kind of category and one shape for all of them, whoever asked
// for it: the defaults below, the dashboard's form and the WhatsApp agent all
// produce the same Slug/Label/Type triple through NewCategory. Default says
// only *where the definition lives* — true means it is written in this file and
// is reconciled onto every user (see ReconcileDefaultCategories), false means
// this user asked for it. It is not a second class of category, and nothing
// downstream may treat it as one.
type Category struct {
	UserID  string
	Slug    string    // e.g. "aluguel" — used as DynamoDB key
	Label   string    // e.g. "Aluguel" — displayed in UI
	Type    EntryType // which entry types this category applies to
	Default bool      // true = defined in this codebase (DefaultCategories)
}

// maxCategorySlugLen bounds a generated slug. It is a DynamoDB sort key
// component, and a label pasted from a supplier's invoice can be a paragraph.
const maxCategorySlugLen = 48

// ErrInvalidCategory is returned by NewCategory when the label or the type
// cannot produce a usable category.
var ErrInvalidCategory = errors.New("invalid category")

// NewCategory builds a user-defined category from a human label, in the exact
// shape the defaults have: a snake_case ASCII slug, the label as typed, and an
// entry type.
//
// It exists so a category created from WhatsApp is indistinguishable from one
// created in the dashboard. The two paths used to build the struct by hand —
// the dashboard took the slug straight from the request body — so nothing
// stopped "Venda Atacado", "venda-atacado" and "vendaAtacado" from becoming
// three categories of the same thing, and every reader downstream would have had
// to cope with all three spellings. The slug is derived here, once, from the
// label; a caller may propose one (slug), but it goes through the same
// normalization.
func NewCategory(userID, label, slug string, entryType EntryType) (Category, error) {
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		return Category{}, ErrInvalidCategory
	}
	if entryType != EntryTypeExpense && entryType != EntryTypeIncome {
		return Category{}, ErrInvalidCategory
	}
	if strings.TrimSpace(slug) == "" {
		slug = label
	}
	normalized := NormalizeCategorySlug(slug)
	if normalized == "" {
		return Category{}, ErrInvalidCategory
	}
	return Category{
		UserID: userID,
		Slug:   normalized,
		Label:  label,
		Type:   entryType,
		// Not a default: the defaults are the ones written in this file. A
		// category someone asked for is still an ordinary category — see the
		// type's doc — but it is not reconciled onto anybody else.
		Default: false,
	}, nil
}

// NormalizeCategorySlug renders any text as a category slug in the one form
// this codebase uses: lowercase ASCII words joined by underscores, accents
// folded ("Empréstimo" → "emprestimo"), everything else dropped.
//
// It is the single definition of "the same pattern as today". Empty output
// means the input had nothing usable in it (punctuation or emoji alone), which
// callers must reject rather than store.
func NormalizeCategorySlug(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastUnderscore := true // leading underscores never get written
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		r = foldAccent(r)
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
		if b.Len() >= maxCategorySlugLen {
			break
		}
	}
	return strings.Trim(b.String(), "_")
}

// accentFolds is pt-BR's diacritics, written out rather than pulled from
// golang.org/x/text: the whole need here is a handful of vowels plus ç, and a
// Unicode normalization dependency for that is more surface than the problem.
var accentFolds = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

func foldAccent(r rune) rune {
	if folded, ok := accentFolds[r]; ok {
		return folded
	}
	return r
}

// CategoryLabelFromSlug is the label to show when the real one is not at hand:
// the slug title-cased, "venda_atacado" → "Venda Atacado".
//
// It is a fallback and never the first choice. A category's label is whatever
// the user typed, accents and all, and it lives in their catalog — this only
// keeps a rendering from printing a raw slug at somebody when the catalog could
// not be read or the entry names a category that no longer exists.
func CategoryLabelFromSlug(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "_", " "), " ")
	for i, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// CategoryLabels indexes a catalog by slug, so a rendering can name a category
// the way its owner named it. Missing slugs are the caller's to fall back on —
// see CategoryLabelFromSlug.
func CategoryLabels(cats []Category) map[string]string {
	labels := make(map[string]string, len(cats))
	for _, c := range cats {
		labels[c.Slug] = c.Label
	}
	return labels
}

// needsCategoryUpdate reports whether a stored default category diverged from
// the current definition. If Category later gains a synced field (Icon, Color,
// Order), this is the single place to extend.
func needsCategoryUpdate(current, expected Category) bool {
	return current.Label != expected.Label ||
		current.Type != expected.Type ||
		current.Default != expected.Default
}

// ReconcileDefaultCategories returns the default categories that should be
// upserted so the user's defaults match the current definitions: missing ones,
// plus defaults whose Label, Type, or Default flag drifted.
//
// Categories the user created themselves (Default == false) are preserved. The
// one exception is a Default=false category that still carries the default's
// exact label and type: that is a default persisted wrong by a bug, not a user
// creation, so its flag is restored.
func ReconcileDefaultCategories(userID string, have []Category) []Category {
	bySlug := make(map[string]Category, len(have))
	for _, c := range have {
		bySlug[c.Slug] = c
	}

	var changes []Category
	for _, d := range DefaultCategories(userID) {
		existing, ok := bySlug[d.Slug]
		if !ok {
			changes = append(changes, d)
			continue
		}
		if existing.Default {
			if needsCategoryUpdate(existing, d) {
				changes = append(changes, d)
			}
			continue
		}
		if existing.Label == d.Label && existing.Type == d.Type {
			changes = append(changes, d)
		}
	}
	return changes
}

// DefaultCategories returns the predefined pharmacy categories.
func DefaultCategories(userID string) []Category {
	expense := []struct{ slug, label string }{
		{"aluguel", "Aluguel"},
		{"folha_pagamento", "Folha de Pagamento"},
		{"fornecedor_medicamentos", "Fornecedor de Medicamentos"},
		{"fornecedor_perfumaria", "Fornecedor de Perfumaria"},
		{"fornecedor_varejo", "Fornecedor Varejo"},
		{"fornecedor_geral", "Fornecedor Geral"},
		{"impostos", "Impostos"},
		{"emprestimo", "Empréstimo"},
		{"cartao_credito", "Cartão de Crédito"},
		{"energia_agua", "Energia / Água"},
		{"telefone_internet", "Telefone / Internet"},
		{"manutencao", "Manutenção"},
		{"outros_despesas", "Outros (Despesa)"},
	}
	income := []struct{ slug, label string }{
		{"venda_balcao", "Venda Balcão"},
		{"venda_atacado", "Venda Atacado"},
		{"convenio", "Convênio"},
		{"delivery", "Delivery"},
		{"outros_receitas", "Outros (Receita)"},
	}

	cats := make([]Category, 0, len(expense)+len(income))
	for _, c := range expense {
		cats = append(cats, Category{
			UserID:  userID,
			Slug:    c.slug,
			Label:   c.label,
			Type:    EntryTypeExpense,
			Default: true,
		})
	}
	for _, c := range income {
		cats = append(cats, Category{
			UserID:  userID,
			Slug:    c.slug,
			Label:   c.label,
			Type:    EntryTypeIncome,
			Default: true,
		})
	}
	return cats
}
