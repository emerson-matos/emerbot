package domain

// Category represents a financial category for organizing entries.
type Category struct {
	UserID  string
	Slug    string    // e.g. "aluguel" — used as DynamoDB key
	Label   string    // e.g. "Aluguel" — displayed in UI
	Type    EntryType // which entry types this category applies to
	Default bool      // true = predefined pharmacy category
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
