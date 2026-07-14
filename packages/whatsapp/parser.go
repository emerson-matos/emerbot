package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
)

// ParsedEntry is the result of parsing a WhatsApp message into financial data.
type ParsedEntry struct {
	Type        domain.EntryType
	Amount      int64 // centavos
	Category    string
	Description string
	DueDate     *time.Time
	IsPending   bool // true if entry should be PaymentStatusPending
}

// Parser extracts financial entries from WhatsApp messages.
type Parser interface {
	Parse(ctx context.Context, text string) (ParsedEntry, error)
}

// GeminiParser uses the Gemini API for natural language parsing with a
// regex-based fallback for well-structured commands.
type GeminiParser struct {
	client *genai.Client
	model  string
}

func NewGeminiParser(ctx context.Context, apiKey string) (*GeminiParser, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}
	return &GeminiParser{client: client, model: "gemini-2.0-flash"}, nil
}

const systemPrompt = `Você é um assistente de extração de dados financeiros para uma farmácia.
Extraia informações da mensagem e retorne JSON com os campos:
- type: "expense" ou "income"
- amount_cents: valor em centavos. R$500,00 = 50000. R$500,10 = 50010. "500" = 50000. "500,1" = 50010. "1500,50" = 150050.
- category: uma de [aluguel, folha_pagamento, fornecedor_medicamentos, fornecedor_geral, impostos, emprestimo, cartao_credito, energia_agua, telefone_internet, manutencao, venda_balcao, convenio, delivery, outros_despesas, outros_receitas]
- description: descrição curta em português
- due_date: data no formato YYYY-MM-DD ou null (hoje se não especificado e o comando for /pagar ou /receber)
- is_pending: true se for /pagar ou /receber (a pagar/a receber), false se for /despesa ou /receita (já ocorreu)

Comandos reconhecidos:
/despesa <valor> <categoria> [descrição]  → despesa já paga
/receita <valor> <categoria> [descrição]  → receita já recebida
/pagar <valor> <categoria> [data] [descrição]   → despesa a pagar (pending)
/receber <valor> <categoria> [data] [descrição] → receita a receber (pending)

Regras de valor:
- "500" ou "500,00" → 50000 centavos (R$500,00)
- "500,1" ou "500,10" → 50010 centavos (R$500,10)
- "1500,50" → 150050 centavos (R$1.500,50)

Responda APENAS com JSON válido, sem markdown, sem explicações.`

type geminiResponse struct {
	Type        string `json:"type"`
	AmountCents int64  `json:"amount_cents"`
	Category    string `json:"category"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"` // "YYYY-MM-DD" or ""
	IsPending   bool   `json:"is_pending"`
}

func (p *GeminiParser) Parse(ctx context.Context, text string) (ParsedEntry, error) {
	// Try regex first for well-structured commands — faster and free.
	if entry, ok := parseRegex(text); ok {
		return entry, nil
	}

	// Fall back to Gemini for natural language.
	contents := []*genai.Content{
		{Parts: []*genai.Part{{Text: systemPrompt + "\n\nMensagem: " + text}}},
	}
	resp, err := p.client.Models.GenerateContent(ctx, p.model, contents, nil)
	if err != nil {
		return ParsedEntry{}, fmt.Errorf("gemini generate: %w", err)
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return ParsedEntry{}, fmt.Errorf("gemini returned empty response")
	}

	raw := strings.TrimSpace(resp.Candidates[0].Content.Parts[0].Text)
	// Strip markdown code fences if Gemini adds them despite instructions.
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var gr geminiResponse
	if err := json.Unmarshal([]byte(raw), &gr); err != nil {
		return ParsedEntry{}, fmt.Errorf("parse gemini response %q: %w", raw, err)
	}

	return geminiResponseToParsed(gr)
}

func geminiResponseToParsed(gr geminiResponse) (ParsedEntry, error) {
	entryType := domain.EntryTypeExpense
	if gr.Type == "income" {
		entryType = domain.EntryTypeIncome
	}

	var dueDate *time.Time
	if gr.DueDate != "" {
		t, err := time.Parse("2006-01-02", gr.DueDate)
		if err == nil {
			dueDate = &t
		}
	}

	if gr.AmountCents <= 0 {
		return ParsedEntry{}, fmt.Errorf("invalid amount: %d", gr.AmountCents)
	}

	return ParsedEntry{
		Type:        entryType,
		Amount:      gr.AmountCents,
		Category:    gr.Category,
		Description: gr.Description,
		DueDate:     dueDate,
		IsPending:   gr.IsPending,
	}, nil
}

// RegexParser implements Parser using only regex (no Gemini API key needed).
// Useful for local development or when no API key is configured.
type RegexParser struct{}

func NewRegexParser() *RegexParser {
	return &RegexParser{}
}

func (p *RegexParser) Parse(_ context.Context, text string) (ParsedEntry, error) {
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

	amount, err := parseAmount(amountStr)
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

	// Try to extract a date from the rest of the string.
	var dueDate *time.Time
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
		dueDate = &t
		desc = strings.TrimSpace(strings.Replace(rest, dm[0], "", 1))
	}

	if desc == "" {
		desc = humanCategory(category)
	}

	return ParsedEntry{
		Type:        entryType,
		Amount:      amount,
		Category:    category,
		Description: desc,
		DueDate:     dueDate,
		IsPending:   isPending,
	}, true
}

// parseAmount converts "500", "500.10", "1500.50" → centavos.
// "500" → 50000, "500.1" → 50010, "500.10" → 50010, "1500.50" → 150050.
func parseAmount(s string) (int64, error) {
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
