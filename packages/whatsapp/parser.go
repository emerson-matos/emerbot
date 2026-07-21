package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

// ParsedEntry is the result of parsing a WhatsApp message into financial data.
type ParsedEntry struct {
	Type        domain.EntryType
	Amount      int64 // centavos
	Category    string
	Description string
	// Date is the transaction date the user specified for an already-occurred
	// entry (/despesa, /receita). Nil means "today".
	Date *time.Time
	// DueDate is the due date for a pending entry (/pagar, /receber). Nil
	// means "today" (the entry is due immediately / date wasn't given).
	DueDate   *time.Time
	IsPending bool // true if entry should be PaymentStatusPending
}

// RegexParser extracts financial entries from well-structured slash commands
// using only regex (no Gemini API key needed). Free-text natural language is
// handled by GeminiAgent instead; this is the fast, free path for commands like
// "/despesa 500 aluguel".
type RegexParser struct{}

func NewRegexParser() *RegexParser {
	return &RegexParser{}
}

func (p *RegexParser) Parse(_ context.Context, text string, _ time.Time) (ParsedEntry, error) {
	entry, ok := parseRegex(text)
	if !ok {
		return ParsedEntry{}, fmt.Errorf("could not parse command, use format: /despesa 500 aluguel")
	}
	return entry, nil
}

// --- Regex parser for structured commands ---

// commandPattern matches: /command amount [category] [rest]
// Examples:
//
//	/despesa 500 aluguel julho
//	/pagar 1500,50 fornecedor_medicamentos 20/07
//	/receita 800 venda_balcao
var commandPattern = regexp.MustCompile(
	`(?i)^/(despesa|receita|pagar|receber)\s+(\d+(?:[,.]\d{1,2})?)\s*(\S+)?(.*)$`,
)

// datePatterns matches dd/mm, dd/mm/yy, dd/mm/yyyy
var datePattern = regexp.MustCompile(`(\d{1,2})/(\d{1,2})(?:/(\d{2,4}))?`)

func parseRegex(text string) (ParsedEntry, bool) {
	m := commandPattern.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return ParsedEntry{}, false
	}

	cmd := strings.ToLower(m[1])
	amountStr := strings.ReplaceAll(m[2], ",", ".")
	category := strings.ToLower(m[3])
	rest := strings.TrimSpace(m[4])

	amount, err := ParseAmount(amountStr)
	if err != nil || amount <= 0 {
		return ParsedEntry{}, false
	}

	entryType := domain.EntryTypeExpense
	isPending := false
	if cmd == "receita" || cmd == "receber" {
		entryType = domain.EntryTypeIncome
	}
	if cmd == "pagar" || cmd == "receber" {
		isPending = true
	}

	if category == "" {
		category = defaultCategory(entryType)
	}

	// Try to extract a date from the rest of the string. For pending commands
	// (/pagar, /receber) this is the due date; for already-occurred ones
	// (/despesa, /receita) it's the actual transaction date.
	var parsedDate *time.Time
	desc := rest
	if dm := datePattern.FindStringSubmatch(rest); dm != nil {
		day, _ := strconv.Atoi(dm[1])
		month, _ := strconv.Atoi(dm[2])
		year := time.Now().Year()
		if dm[3] != "" {
			y, err := strconv.Atoi(dm[3])
			if err == nil {
				if y < 100 {
					y += 2000
				}
				year = y
			}
		}
		t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		parsedDate = &t
		desc = strings.TrimSpace(strings.Replace(rest, dm[0], "", 1))
	}

	if desc == "" {
		desc = humanCategory(category)
	}

	entry := ParsedEntry{
		Type:        entryType,
		Amount:      amount,
		Category:    category,
		Description: desc,
		IsPending:   isPending,
	}
	if isPending {
		entry.DueDate = parsedDate
	} else {
		entry.Date = parsedDate
	}
	return entry, true
}

// ParseAmount converts "500", "500.10", "1500.50" → centavos.
// "500" → 50000, "500.1" → 50010, "500.10" → 50010, "1500.50" → 150050.
func ParseAmount(s string) (int64, error) {
	parts := strings.SplitN(s, ".", 2)
	reais, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	var centavos int64
	if len(parts) == 2 {
		c := parts[1]
		if len(c) == 1 {
			c += "0" // "500.1" → "10" centavos
		}
		centavos, err = strconv.ParseInt(c, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return reais*100 + centavos, nil
}

func defaultCategory(t domain.EntryType) string {
	if t == domain.EntryTypeIncome {
		return "outros_receitas"
	}
	return "outros_despesas"
}

func humanCategory(slug string) string {
	labels := map[string]string{
		"aluguel":                 "Aluguel",
		"folha_pagamento":         "Folha de Pagamento",
		"fornecedor_medicamentos": "Fornecedor de Medicamentos",
		"fornecedor_geral":        "Fornecedor Geral",
		"impostos":                "Impostos",
		"emprestimo":              "Empréstimo",
		"cartao_credito":          "Cartão de Crédito",
		"energia_agua":            "Energia / Água",
		"telefone_internet":       "Telefone / Internet",
		"manutencao":              "Manutenção",
		"venda_balcao":            "Venda Balcão",
		"convenio":                "Convênio",
		"delivery":                "Delivery",
		"outros_despesas":         "Outros",
		"outros_receitas":         "Outros",
	}
	if l, ok := labels[slug]; ok {
		return l
	}
	return slug
}
