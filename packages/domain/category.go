package domain

// Category represents a financial category for organizing entries.
type Category struct {
	UserID  string
	Slug    string    // e.g. "aluguel" — used as DynamoDB key
	Label   string    // e.g. "Aluguel" — displayed in UI
	Type    EntryType // which entry types this category applies to
	Default bool      // true = predefined pharmacy category
}

// DefaultCategories returns the predefined pharmacy categories.
func DefaultCategories(userID string) []Category {
	expense := []struct{ slug, label string }{
		{"aluguel", "Aluguel"},
		{"folha_pagamento", "Folha de Pagamento"},
		{"fornecedor_medicamentos", "Fornecedor de Medicamentos"},
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
