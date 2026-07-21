package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
)

// ErrNotFinancial is returned by GeminiParser.Parse when the model determines
// the message isn't a financial entry or command at all (greeting, question,
// chit-chat). Callers should show a friendly hint instead of a parse-error
// message.
var ErrNotFinancial = errors.New("message is not a financial entry")

// geminiTimeout bounds how long a single Gemini call may take, so a slow or
// hanging API call never stalls the webhook handler indefinitely.
const geminiTimeout = 10 * time.Second

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

// Parser extracts financial entries from WhatsApp messages.
type Parser interface {
	Parse(ctx context.Context, text string, msgTime time.Time) (ParsedEntry, error)
}

const geminiModel = "gemini-3.1-flash-lite"

// contentGenerator is the slice of *genai.Models the parser needs; it lets
// tests inject a fake without network access.
type contentGenerator interface {
	GenerateContent(ctx context.Context, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

// GeminiParser uses the Gemini API for natural language parsing with a
// regex-based fallback for well-structured commands.
type GeminiParser struct {
	gen   contentGenerator
	model string
}

func NewGeminiParser(ctx context.Context, apiKey string) (*GeminiParser, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini client: %w", err)
	}
	return &GeminiParser{gen: client.Models, model: geminiModel}, nil
}

func buildSystemPrompt(now time.Time) string {
	return fmt.Sprintf(
		`Você é um assistente de extração de dados financeiros para uma farmácia.

Contexto atual:
- Hoje é %s
- Fuso horário: America/Sao_Paulo

Interprete datas relativas ("amanhã", "último dia do mês", "mês que vem", etc.)
usando a data acima como referência. Nunca invente datas.
Se a mensagem contém uma data explícita, preserve-a exatamente.

Extraia informações da mensagem e retorne JSON com os campos:
- type: "expense" ou "income"
- amount_cents: valor em centavos. R$500,00 = 50000. R$500,10 = 50010. "500" = 50000. "500,1" = 50010. "1500,50" = 150050.
- category: uma de [aluguel, folha_pagamento, fornecedor_medicamentos, fornecedor_geral, impostos, emprestimo, cartao_credito, energia_agua, telefone_internet, manutencao, venda_balcao, convenio, delivery, outros_despesas, outros_receitas]
- description: descrição curta em português
- due_date: data no formato YYYY-MM-DD ou null.
  - Se o comando for /pagar ou /receber: é a data de vencimento (hoje se não especificada).
  - Se o comando for /despesa ou /receita: é a data em que a transação realmente ocorreu, se mencionada na mensagem (null se não mencionada — assume-se hoje).
- is_pending: true se for /pagar ou /receber (a pagar/a receber), false se for /despesa ou /receita (já ocorreu)
- is_financial: true se a mensagem descreve um lançamento financeiro (um gasto,
  uma receita, uma conta a pagar/receber) ou um dos comandos abaixo. false se a
  mensagem for uma saudação, pergunta, ou qualquer papo que não seja um
  lançamento financeiro — nesse caso os demais campos podem ficar vazios/zero.

Comandos reconhecidos:
/despesa <valor> <categoria> [data] [descrição]  → despesa já paga
/receita <valor> <categoria> [data] [descrição]  → receita já recebida
/pagar <valor> <categoria> [data] [descrição]   → despesa a pagar (pending)
/receber <valor> <categoria> [data] [descrição] → receita a receber (pending)

Regras de valor:
- "500" ou "500,00" → 50000 centavos (R$500,00)
- "500,1" ou "500,10" → 50010 centavos (R$500,10)
- "1500,50" → 150050 centavos (R$1.500,50)`,
		now.Format("02/01/2006"),
	)
}

type geminiResponse struct {
	Type        string `json:"type"`
	AmountCents int64  `json:"amount_cents"`
	Category    string `json:"category"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"` // "YYYY-MM-DD" or ""
	IsPending   bool   `json:"is_pending"`
	IsFinancial bool   `json:"is_financial"`
}

// financialCategories lists the closed set of categories the model may pick
// from — kept in sync with the enum described in systemPrompt.
var financialCategories = []string{
	"aluguel", "folha_pagamento", "fornecedor_medicamentos", "fornecedor_geral",
	"impostos", "emprestimo", "cartao_credito", "energia_agua",
	"telefone_internet", "manutencao", "venda_balcao", "convenio", "delivery",
	"outros_despesas", "outros_receitas",
}

// geminiResponseSchema mirrors geminiResponse and is passed as structured
// output config, so Gemini returns well-formed JSON instead of relying on
// prompt instructions alone.
var geminiResponseSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"type":         {Type: genai.TypeString, Enum: []string{"expense", "income"}},
		"amount_cents": {Type: genai.TypeInteger},
		"category":     {Type: genai.TypeString, Enum: financialCategories},
		"description":  {Type: genai.TypeString},
		"due_date":     {Type: genai.TypeString, Nullable: genai.Ptr(true)},
		"is_pending":   {Type: genai.TypeBoolean},
		"is_financial": {Type: genai.TypeBoolean},
	},
	Required: []string{"type", "amount_cents", "category", "description", "is_pending", "is_financial"},
}

func (p *GeminiParser) Parse(ctx context.Context, text string, msgTime time.Time) (ParsedEntry, error) {
	// Try regex first for well-structured commands — faster and free.
	if entry, ok := parseRegex(text); ok {
		return entry, nil
	}

	ctx, cancel := context.WithTimeout(ctx, geminiTimeout)
	defer cancel()

	// Fall back to Gemini for natural language. Build the config per-call so
	// the system instruction includes the current date as a temporal reference.
	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: buildSystemPrompt(msgTime)}}},
		ResponseMIMEType:  "application/json",
		ResponseSchema:    geminiResponseSchema,
		Temperature:       genai.Ptr[float32](0),
		MaxOutputTokens:   256,
	}

	contents := []*genai.Content{
		{Parts: []*genai.Part{{Text: text}}},
	}
	resp, err := p.gen.GenerateContent(ctx, p.model, contents, config)
	if err != nil {
		return ParsedEntry{}, fmt.Errorf("gemini generate: %w", err)
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return ParsedEntry{}, fmt.Errorf("gemini returned empty response")
	}

	raw := strings.TrimSpace(resp.Candidates[0].Content.Parts[0].Text)
	// Strip markdown code fences as a safety net, in case Gemini adds them
	// despite the structured JSON response mode.
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var gr geminiResponse
	if err := json.Unmarshal([]byte(raw), &gr); err != nil {
		return ParsedEntry{}, fmt.Errorf("parse gemini response %q: %w", raw, err)
	}

	if !gr.IsFinancial {
		return ParsedEntry{}, ErrNotFinancial
	}

	return geminiResponseToParsed(gr, msgTime)
}

func geminiResponseToParsed(gr geminiResponse, reference time.Time) (ParsedEntry, error) {
	entryType := domain.EntryTypeExpense
	if gr.Type == "income" {
		entryType = domain.EntryTypeIncome
	}

	var parsedDate *time.Time
	if gr.DueDate != "" {
		t, err := time.Parse("2006-01-02", gr.DueDate)
		if err == nil {
			// Validate the date is within a reasonable range around the
			// reference date, to catch hallucinated years.
			y := t.Year()
			ry := reference.Year()
			if y < ry-1 || y > ry+2 {
				return ParsedEntry{}, fmt.Errorf(
					"date out of range: %s (reference: %s)",
					t.Format("2006-01-02"),
					reference.Format("2006-01-02"),
				)
			}
			parsedDate = &t
		}
	}

	if gr.AmountCents <= 0 {
		return ParsedEntry{}, fmt.Errorf("invalid amount: %d", gr.AmountCents)
	}

	entry := ParsedEntry{
		Type:        entryType,
		Amount:      gr.AmountCents,
		Category:    gr.Category,
		Description: gr.Description,
		IsPending:   gr.IsPending,
	}
	// The date Gemini extracts means different things depending on the
	// command: a due date for pending entries, the transaction date otherwise.
	if gr.IsPending {
		entry.DueDate = parsedDate
	} else {
		entry.Date = parsedDate
	}
	return entry, nil
}

// RegexParser implements Parser using only regex (no Gemini API key needed).
// Useful for local development or when no API key is configured.
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
