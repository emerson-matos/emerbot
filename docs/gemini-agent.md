# GeminiAgent — implementação

| Fase | Status |
|------|--------|
| 1 — Prompt dinâmico + validação de data | ✅ Implementada (PR #21) |
| 2 — GeminiAgent com function calling | ✅ Implementada |

---

## Fase 1 — Prompt dinâmico + validação de data

**Status: implementada** no PR #21 (`gemini-refactor`).

**Objetivo**: o Gemini saber que "hoje" é 21/07/2026 e não alucinar 31/05/2025.

### Arquivos alterados

| Arquivo | Mudança |
|---------|---------|
| `packages/whatsapp/parser.go` | `Parser.Parse` recebe `msgTime`; `systemPrompt` vira `buildSystemPrompt(now)`; `geminiConfig` por chamada; validação de ano |
| `apps/webhook/internal/financial/handler.go` | `Handle` recebe `msgTime` e repassa ao parser |
| `apps/webhook/internal/app/app.go` | Passa `message.Timestamp` (do webhook da Meta) |
| Testes | Chamadas de `Parse` e `Handle` ajustadas; `fakeParser`/`fakeNLParser` atualizados |

### O que foi feito

### 1. `packages/whatsapp/parser.go`

#### a. `Parser` interface (linha 44)

Adicionar `msgTime time.Time`:

```go
type Parser interface {
    Parse(ctx context.Context, text string, msgTime time.Time) (ParsedEntry, error)
}
```

#### b. `systemPrompt` de `const` para função (linha 74)

Substituir:

```go
const systemPrompt = `Você é um assistente...`
```

Por:

```go
func buildSystemPrompt(now time.Time) string {
    const base = `Você é um assistente de extração de dados financeiros para uma farmácia.

Contexto atual:
- Hoje é %s
- Fuso horário: America/Sao_Paulo

Interprete datas relativas usando a data acima como referência.
Nunca invente datas. Se a mensagem contém uma data explícita,
preserve-a exatamente.

Extraia informações da mensagem...` +
    // restante do prompt (campos, regras de valor, categorias) – igual ao
    // conteúdo atual da const systemPrompt, exceto a remoção da data fixa.

    return fmt.Sprintf(base, now.Format("02/01/2006"))
}
```

#### c. `geminiConfig` global → por chamada (linha 139)

Substituir:

```go
var geminiConfig = &genai.GenerateContentConfig{...}
```

Por construção dentro de `Parse()`:

```go
config := &genai.GenerateContentConfig{
    SystemInstruction: &genai.Content{
        Parts: []*genai.Part{{Text: buildSystemPrompt(msgTime)}},
    },
    ResponseMIMEType: "application/json",
    ResponseSchema:   geminiResponseSchema,
    Temperature:      genai.Ptr[float32](0),
    MaxOutputTokens:  256,
}
```

#### d. `Parse()` recebe `msgTime` (linha 147)

```go
func (p *GeminiParser) Parse(ctx context.Context, text string, msgTime time.Time) (ParsedEntry, error) {
    if entry, ok := parseRegex(text); ok {
        return entry, nil
    }
    // ...
    config := &genai.GenerateContentConfig{
        SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: buildSystemPrompt(msgTime)}}},
        // ...
    }
    // ...
}
```

#### e. `geminiResponseToParsed` com validação (linha 189)

Adicionar parâmetro `reference time.Time` e validar ano:

```go
func geminiResponseToParsed(gr geminiResponse, reference time.Time) (ParsedEntry, error) {
    // ... (código existente) ...
    if parsedDate != nil {
        y := parsedDate.Year()
        ry := reference.Year()
        if y < ry-1 || y > ry+2 {
            return ParsedEntry{}, fmt.Errorf(
                "data fora do intervalo: %s (hoje é %s)",
                parsedDate.Format("2006-01-02"),
                reference.Format("2006-01-02"),
            )
        }
    }
    // ...
}
```

#### f. `RegexParser.Parse` ajusta assinatura (linha 232)

```go
func (p *RegexParser) Parse(_ context.Context, text string, _ time.Time) (ParsedEntry, error) {
```

### 2. `apps/webhook/internal/financial/handler.go`

#### a. `Handle` recebe `msgTime` (linha 80)

```go
func (h *Handler) Handle(ctx context.Context, userID, text string, msgTime time.Time) (string, error) {
```

Linha 87:

```go
parsed, err := h.parser.Parse(ctx, text, msgTime)
```

### 3. `apps/webhook/internal/app/app.go`

#### a. Passar `message.Timestamp` (linhas 278 e 296)

Onde chama `handler.Handle(ctx, ledgerID, text)`, passar `message.Timestamp`:

```go
reply, err := a.financialHandler.Handle(ctx, ledgerID, text, message.Timestamp)
```

O `message.Timestamp` já é `time.Time` vindo do webhook da Meta (via
`normalize` → `waTimestamp` → `time.Unix`).

### 4. Testes

#### `packages/whatsapp/gemini_parser_test.go`

Toda chamada a `parser.Parse(ctx, text)` vira `parser.Parse(ctx, text, time.Now())`.

#### `packages/whatsapp/parser_test.go`

Idem.

### 5. Verificação

```bash
make test          # todos os testes passam
make build         # compila
```

---

## Fase 2 — GeminiAgent com function calling

**Status: implementada.** O `GeminiParser` e a interface `Parser` foram removidos;
um agente de function calling trata todo o fluxo de linguagem natural (hoje
`Agent` em `packages/orchestrator/internal/gemini/agent.go` — ver "Atualização
(pós-implementação)" abaixo), e o `RegexParser` continua como fast path para
slash commands. As tools vivem em `packages/finance/tools.go`.

Diferenças em relação ao rascunho abaixo (o código é a fonte da verdade):

- O modelo é `geminiModel` (`gemini-3.1-flash-lite`), não `gemini-2.5-flash-lite`.
- As tools ficam no pacote `finance` (sem prefixo `pkgfinance`); `FinanceTools`
  retorna `[]finance.Tool` (nome, descrição, schema e handler agrupados), com
  o handler tipado como `finance.ToolFunc`.
- `create_financial_entry` mantém o enum fechado de categorias e arredonda
  reais→centavos (19.99 → 1999).
- O `search_entries` usa o novo campo `EntryFilter.Description` (filtro no store),
  não filtragem em memória no handler.
- O loop do agente coleta **todos** os `FunctionCall` de um turno (não só o
  primeiro) e responde com `{"output": ...}`.
- O `financial.Handler` recebe `(regex *whatsapp.RegexParser, agent Agent, store)`;
  `Agent` é uma interface local (nil = deployment regex-only).

**Atualização (pós-implementação):** o desenho acima foi novamente movido quando
`packages/orchestrator` (ADR-006) passou a centralizar todo o fluxo, não só o
financeiro:

- `GeminiAgent` saiu de `packages/whatsapp/gemini_agent.go` e virou `Agent` em
  `packages/orchestrator/internal/gemini/agent.go`.
- `financial.Handler` (`apps/webhook/internal/financial/handler.go`) não trata
  mais linguagem natural — hoje só resolve slash commands via `RegexParser`
  (`/despesa`, `/receita`, `/pagar`, `/receber`, `/recorrente`, `/resumo`,
  `/goal`, `/meta`).
- O roteamento entre os dois vive em `apps/webhook/internal/app/app.go`:
  `isFinancialCommand` manda comandos com prefixo conhecido para
  `financialHandler`; qualquer outra mensagem vai para `orchestrator.Service`,
  que carrega memória de curto/longo prazo e delega ao `Agent` via
  `TextGenerator` (`orchestrator.NewTextGenerator` escolhe entre o `Agent` real
  e `StaticClient` como fallback quando `GEMINI_API_KEY`/`FinanceStore` não
  estão configurados).

**Objetivo**: substituir `GeminiParser` por um agente que usa tools para tudo.

### Visão geral dos arquivos

> A tabela e o rascunho abaixo são o plano original desta fase, hoje só de
> valor histórico — os arquivos `packages/whatsapp/gemini_agent.go` e o
> `financial.Handler` com `agent` embutido não existem mais; veja a
> "Atualização (pós-implementação)" acima para a localização atual.

| Arquivo | Ação |
|---------|------|
| `packages/whatsapp/gemini_agent.go` | **novo** — `GeminiAgent` com loop de function calling |
| `packages/whatsapp/parser.go` | remover `GeminiParser`, `systemPrompt`, `geminiConfig`, interface `Parser` |
| `packages/whatsapp/gemini_agent_test.go` | **novo** — testes do agente |
| `packages/whatsapp/gemini_parser_test.go` | **remover** |
| `packages/finance/entry_filter.go` | adicionar campo `Description` no `EntryFilter` |
| `packages/finance/dynamodb.go` | adicionar filter expression para descrição |
| `packages/finance/tools.go` | **novo** — declarações das tools |
| `packages/finance/inmemory.go` | suportar filtro por descrição no `ListEntries` |
| `apps/webhook/internal/financial/handler.go` | usar `GeminiAgent` + `RegexParser` |
| `apps/webhook/internal/app/app.go` | wire do `GeminiAgent` |

---

### Passo 1 — Tool definitions (`packages/finance/tools.go` — novo)

```go
package finance

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/emerson/emerbot/packages/domain"
    "google.golang.org/genai"
)

// GeminiTool associa uma FunctionDeclaration ao seu handler.
type GeminiTool struct {
    Decl *genai.FunctionDeclaration
    Fn   func(ctx context.Context, userID string, args json.RawMessage, store Store) (any, error)
}

func FinanceTools(store Store) ([]*genai.Tool, map[string]func(context.Context, string, json.RawMessage) (any, error)) {
    handlers := make(map[string]func(context.Context, string, json.RawMessage) (any, error))

    tools := []*genai.Tool{
        createEntryTool(handlers, store),
        monthSummaryTool(handlers, store),
        listDueEntriesTool(handlers, store),
        searchEntriesTool(handlers, store),
    }

    return tools, handlers
}

// --- create_financial_entry ---

func createEntryTool(handlers map[string]func(context.Context, string, json.RawMessage) (any, error), store Store) *genai.Tool {
    name := "create_financial_entry"

    decl := &genai.FunctionDeclaration{
        Name:        name,
        Description: "Cria um novo lançamento financeiro (despesa, receita, conta a pagar/receber).",
        Parameters: &genai.Schema{
            Type: genai.TypeObject,
            Properties: map[string]*genai.Schema{
                "type":        {Type: genai.TypeString, Enum: []string{"expense", "income"}},
                "amount":      {Type: genai.TypeNumber, Description: "Valor em reais (ex: 500.00)"},
                "category":    {Type: genai.TypeString, Description: "Categoria do lançamento"},
                "description": {Type: genai.TypeString, Description: "Descrição do lançamento"},
                "date":        {Type: genai.TypeString, Description: "Data no formato YYYY-MM-DD (padrão: hoje)"},
                "due_date":    {Type: genai.TypeString, Description: "Data de vencimento YYYY-MM-DD (para contas a pagar/receber)"},
                "is_pending":  {Type: genai.TypeBoolean, Description: "true = a pagar/receber, false = já pago/recebido"},
            },
            Required: []string{"type", "amount", "category", "is_pending"},
        },
    }

    handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
        var args struct {
            Type        string  `json:"type"`
            Amount      float64 `json:"amount"`
            Category    string  `json:"category"`
            Description string  `json:"description"`
            Date        string  `json:"date"`
            DueDate     string  `json:"due_date"`
            IsPending   bool    `json:"is_pending"`
        }
        if err := json.Unmarshal(raw, &args); err != nil {
            return nil, fmt.Errorf("parse args: %w", err)
        }

        now := time.Now().UTC()
        entry := domain.FinancialEntry{
            UserID:      userID,
            EntryID:     uuid.New().String(),
            Date:        now,
            Amount:      int64(args.Amount * 100),
            Category:    args.Category,
            Description: args.Description,
            Source:      "whatsapp",
            CreatedAt:   now,
            UpdatedAt:   now,
        }

        if args.Type == "income" {
            entry.Type = domain.EntryTypeIncome
        } else {
            entry.Type = domain.EntryTypeExpense
        }

        if args.Date != "" {
            if t, err := time.Parse("2006-01-02", args.Date); err == nil {
                entry.Date = t
            }
        }
        if args.DueDate != "" {
            if t, err := time.Parse("2006-01-02", args.DueDate); err == nil {
                entry.DueDate = &t
            }
        }
        if args.IsPending {
            entry.PaymentStatus = domain.PaymentStatusPending
        } else {
            entry.PaymentStatus = domain.PaymentStatusPaid
            entry.PaymentDate = &entry.Date
        }

        if err := store.SaveEntry(ctx, entry); err != nil {
            return nil, fmt.Errorf("save entry: %w", err)
        }

        return map[string]any{
            "entry_id": entry.EntryID,
            "status":   "created",
        }, nil
    }

    return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- get_month_summary ---

func monthSummaryTool(handlers map[string]func(context.Context, string, json.RawMessage) (any, error), store Store) *genai.Tool {
    name := "get_month_summary"

    decl := &genai.FunctionDeclaration{
        Name:        name,
        Description: "Retorna o resumo financeiro de um mês: receitas, despesas e saldo.",
        Parameters: &genai.Schema{
            Type: genai.TypeObject,
            Properties: map[string]*genai.Schema{
                "month": {Type: genai.TypeString, Description: "Mês no formato YYYY-MM (padrão: mês atual)"},
            },
        },
    }

    handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
        var args struct {
            Month string `json:"month"`
        }
        if err := json.Unmarshal(raw, &args); err != nil {
            return nil, fmt.Errorf("parse args: %w", err)
        }
        if args.Month == "" {
            args.Month = time.Now().UTC().Format("2006-01")
        }

        summary, err := store.MonthlySummary(ctx, userID, args.Month)
        if err != nil {
            return nil, fmt.Errorf("monthly summary: %w", err)
        }

        return map[string]any{
            "month":    summary.Month,
            "income":   float64(summary.TotalIncome) / 100,
            "expense":  float64(summary.TotalExpense) / 100,
            "balance":  float64(summary.Balance) / 100,
        }, nil
    }

    return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- list_due_entries ---

func listDueEntriesTool(handlers map[string]func(context.Context, string, json.RawMessage) (any, error), store Store) *genai.Tool {
    name := "list_due_entries"

    decl := &genai.FunctionDeclaration{
        Name:        name,
        Description: "Lista contas a pagar ou receber em um período de datas.",
        Parameters: &genai.Schema{
            Type: genai.TypeObject,
            Properties: map[string]*genai.Schema{
                "from":   {Type: genai.TypeString, Description: "Data inicial YYYY-MM-DD"},
                "to":     {Type: genai.TypeString, Description: "Data final YYYY-MM-DD"},
                "status": {Type: genai.TypeString, Enum: []string{"pending", "paid"}},
                "limit":  {Type: genai.TypeInteger, Description: "Máximo de resultados (padrão: 20)"},
            },
        },
    }

    handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
        var args struct {
            From   string `json:"from"`
            To     string `json:"to"`
            Status string `json:"status"`
            Limit  int    `json:"limit"`
        }
        if err := json.Unmarshal(raw, &args); err != nil {
            return nil, fmt.Errorf("parse args: %w", err)
        }

        filter := pkgfinance.EntryFilter{Limit: args.Limit}
        if args.Limit == 0 {
            filter.Limit = 20
        }
        if args.From != "" {
            t, _ := time.Parse("2006-01-02", args.From)
            filter.From = &t
        }
        if args.To != "" {
            t, _ := time.Parse("2006-01-02", args.To)
            filter.To = &t
        }
        if args.Status == "pending" {
            filter.Status = domain.PaymentStatusPending
        } else if args.Status == "paid" {
            filter.Status = domain.PaymentStatusPaid
        }

        entries, err := store.ListEntries(ctx, userID, filter)
        if err != nil {
            return nil, fmt.Errorf("list entries: %w", err)
        }

        results := make([]map[string]any, 0, len(entries))
        for _, e := range entries {
            results = append(results, entryToMap(e))
        }
        return results, nil
    }

    return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

// --- search_entries ---

func searchEntriesTool(handlers map[string]func(context.Context, string, json.RawMessage) (any, error), store Store) *genai.Tool {
    name := "search_entries"

    decl := &genai.FunctionDeclaration{
        Name:        name,
        Description: "Busca lançamentos por descrição, categoria ou período.",
        Parameters: &genai.Schema{
            Type: genai.TypeObject,
            Properties: map[string]*genai.Schema{
                "query":    {Type: genai.TypeString, Description: "Texto para buscar na descrição"},
                "category": {Type: genai.TypeString, Description: "Filtrar por categoria"},
                "from":     {Type: genai.TypeString, Description: "Data inicial YYYY-MM-DD"},
                "to":       {Type: genai.TypeString, Description: "Data final YYYY-MM-DD"},
                "limit":    {Type: genai.TypeInteger, Description: "Máximo de resultados (padrão: 20)"},
            },
        },
    }

    handlers[name] = func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
        var args struct {
            Query    string `json:"query"`
            Category string `json:"category"`
            From     string `json:"from"`
            To       string `json:"to"`
            Limit    int    `json:"limit"`
        }
        if err := json.Unmarshal(raw, &args); err != nil {
            return nil, fmt.Errorf("parse args: %w", err)
        }

        filter := pkgfinance.EntryFilter{
            Category: args.Category,
            Limit:    args.Limit,
        }
        if args.Limit == 0 {
            filter.Limit = 20
        }
        if args.From != "" {
            t, _ := time.Parse("2006-01-02", args.From)
            filter.From = &t
        }
        if args.To != "" {
            t, _ := time.Parse("2006-01-02", args.To)
            filter.To = &t
        }

        entries, err := store.ListEntries(ctx, userID, filter)
        if err != nil {
            return nil, fmt.Errorf("search entries: %w", err)
        }

        // Filtro por descrição em memória (até adicionar no EntryFilter)
        if args.Query != "" {
            q := strings.ToLower(args.Query)
            filtered := make([]domain.FinancialEntry, 0, len(entries))
            for _, e := range entries {
                if strings.Contains(strings.ToLower(e.Description), q) {
                    filtered = append(filtered, e)
                }
            }
            entries = filtered
        }

        // Limita após filtro
        if filter.Limit > 0 && len(entries) > filter.Limit {
            entries = entries[:filter.Limit]
        }

        results := make([]map[string]any, 0, len(entries))
        for _, e := range entries {
            results = append(results, entryToMap(e))
        }
        return results, nil
    }

    return &genai.Tool{FunctionDeclarations: []*genai.FunctionDeclaration{decl}}
}

func entryToMap(e domain.FinancialEntry) map[string]any {
    m := map[string]any{
        "entry_id":    e.EntryID,
        "type":        string(e.Type),
        "amount":      float64(e.Amount) / 100,
        "category":    e.Category,
        "description": e.Description,
        "date":        e.Date.Format("2006-01-02"),
        "status":      string(e.PaymentStatus),
    }
    if e.DueDate != nil {
        m["due_date"] = e.DueDate.Format("2006-01-02")
    }
    return m
}
```

### Passo 2 — `EntryFilter` com descrição (`packages/finance/store.go`)

```go
type EntryFilter struct {
    From        *time.Time
    To          *time.Time
    Category    string
    Description string // <-- NOVO: busca por substring na descrição
    Status      domain.PaymentStatus
    Type        domain.EntryType
    Cursor      string
    Limit       int
}
```

### Passo 3 — Filtro no DynamoDB (`packages/finance/dynamodb.go`)

No `ListEntries`, adicionar filter expression:

```go
// Ao construir o filter expression, adicionar:
if filter.Description != "" {
    expr.Add("contains(#desc, :desc)", map[string]string{"#desc": "Description"})
    expr.Add(map[string]any{":desc": filter.Description})
}
```

### Passo 4 — `GeminiAgent` (`packages/whatsapp/gemini_agent.go` — novo)

```go
package whatsapp

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    "google.golang.org/genai"

    "github.com/emerson/emerbot/packages/finance"
)

type GeminiAgent struct {
    gen             contentGenerator
    model           string
    tools           []*genai.Tool
    toolHandlers    map[string]func(ctx context.Context, userID string, args json.RawMessage) (any, error)
}

func NewGeminiAgent(ctx context.Context, apiKey string, store finance.Store) (*GeminiAgent, error) {
    client, err := genai.NewClient(ctx, &genai.ClientConfig{
        APIKey:  apiKey,
        Backend: genai.BackendGeminiAPI,
    })
    if err != nil {
        return nil, fmt.Errorf("create gemini client: %w", err)
    }

    tools, handlers := finance.FinanceTools(store)

    return &GeminiAgent{
        gen:          client.Models,
        model:        "gemini-2.5-flash-lite",
        tools:        tools,
        toolHandlers: handlers,
    }, nil
}

func (a *GeminiAgent) Process(ctx context.Context, userID, text string, msgTime time.Time) (string, error) {
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    systemPrompt := fmt.Sprintf(
        `Você é um assistente financeiro de uma farmácia.
Sua função é ajudar o usuário a gerenciar o fluxo de caixa.

Contexto atual:
- Hoje é %s
- Fuso horário: America/Sao_Paulo

Você tem acesso a ferramentas para criar lançamentos, consultar resumos,
listar contas a pagar/receber e buscar lançamentos.

Regras:
- Sempre use as ferramentas quando precisar de dados. Nunca invente.
- Responda em português, de forma clara e direta.
- Se a mensagem não for financeira, responda educadamente que você é
  um assistente financeiro e pode ajudar com o fluxo de caixa.
- Valores em reais (R$).`,
        msgTime.Format("02/01/2006"),
    )

    config := &genai.GenerateContentConfig{
        SystemInstruction: &genai.Content{Parts: []*genai.Part{{Text: systemPrompt}}},
        Tools:             a.tools,
        Temperature:       genai.Ptr[float32](0),
        MaxOutputTokens:   1024,
    }

    contents := []*genai.Content{
        {Parts: []*genai.Part{{Text: text}}},
    }

    for turn := 0; turn < 5; turn++ {
        resp, err := a.gen.GenerateContent(ctx, a.model, contents, config)
        if err != nil {
            return "", fmt.Errorf("gemini generate (turn %d): %w", turn, err)
        }
        if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
            return "", fmt.Errorf("gemini empty response (turn %d)", turn)
        }

        candidate := resp.Candidates[0].Content

        // Se tiver FunctionCall, executa e continua
        if candidate.Parts[0].FunctionCall != nil {
            fc := candidate.Parts[0].FunctionCall

            handler, ok := a.toolHandlers[fc.Name]
            if !ok {
                return "", fmt.Errorf("unknown tool: %s", fc.Name)
            }

            argsJSON, err := json.Marshal(fc.Args)
            if err != nil {
                return "", fmt.Errorf("marshal function call args: %w", err)
            }

            result, err := handler(ctx, userID, argsJSON)
            if err != nil {
                return "", fmt.Errorf("tool %s: %w", fc.Name, err)
            }

            resultJSON, err := json.Marshal(result)
            if err != nil {
                return "", fmt.Errorf("marshal tool result: %w", err)
            }

            contents = append(contents, candidate)
            contents = append(contents, &genai.Content{
                Parts: []*genai.Part{{
                    FunctionResponse: &genai.FunctionResponse{
                        Name:     fc.Name,
                        Response: map[string]any{"result": string(resultJSON)},
                    },
                }},
            })
            continue
        }

        // Texto puro → resposta final
        text := strings.TrimSpace(candidate.Parts[0].Text)
        if text != "" {
            return text, nil
        }
    }

    return "", fmt.Errorf("agent: too many function call rounds")
}
```

### Passo 5 — Handler adaptado (`apps/webhook/internal/financial/handler.go`)

```go
type Handler struct {
    regexParser *whatsapp.RegexParser
    agent       *whatsapp.GeminiAgent
    store       pkgfinance.Store
}

func NewHandler(regexParser *whatsapp.RegexParser, agent *whatsapp.GeminiAgent, store pkgfinance.Store) *Handler {
    return &Handler{
        regexParser: regexParser,
        agent:       agent,
        store:       store,
    }
}

// regexParseCommand tenta extrair um lançamento de um comando com barra.
// Retorna o entry e true se conseguiu.
func regexParseCommand(text string) (whatsapp.ParsedEntry, bool) {
    return parseRegex(text) // função já existente em parser.go
}
```

**Handle**:

```go
func (h *Handler) Handle(ctx context.Context, userID, text string, msgTime time.Time) (string, error) {
    // Tutorial de comando (igual ao atual)
    if usage := bareCommandUsage(text); usage != "" {
        return usage, nil
    }

    // Slash command → regex fast path (sem LLM)
    if strings.HasPrefix(text, "/") {
        entry, ok := regexParseCommand(text)
        if !ok {
            return "", fmt.Errorf("não consegui entender. Use /help para ver os comandos.")
        }
        return h.saveAndConfirm(ctx, userID, entry), nil
    }

    // NL → agent com tools
    if h.agent != nil {
        reply, err := h.agent.Process(ctx, userID, text, msgTime)
        if err != nil {
            return "❌ Não consegui processar sua mensagem. Tente /help.", nil
        }
        return reply, nil
    }

    return "🤖 Sou um assistente financeiro. Envie /help para ver os comandos.", nil
}

func (h *Handler) saveAndConfirm(ctx context.Context, userID string, parsed whatsapp.ParsedEntry) string {
    now := time.Now().UTC()
    date := now
    if parsed.Date != nil {
        date = *parsed.Date
    }

    status := domain.PaymentStatusPaid
    if parsed.IsPending {
        status = domain.PaymentStatusPending
    }

    entry := domain.FinancialEntry{
        UserID:        userID,
        EntryID:       uuid.New().String(),
        Date:          date,
        Amount:        parsed.Amount,
        Category:      parsed.Category,
        Type:          parsed.Type,
        Description:   parsed.Description,
        DueDate:       parsed.DueDate,
        PaymentStatus: status,
        Source:        "whatsapp",
        CreatedAt:     now,
        UpdatedAt:     now,
    }
    if status == domain.PaymentStatusPaid {
        entry.PaymentDate = &date
    }

    if err := h.store.SaveEntry(ctx, entry); err != nil {
        return "❌ Não consegui salvar. Tente novamente."
    }

    return formatConfirmation(entry)
}
```

**Remover** do handler: o `parser whatsapp.Parser` field, `NewHandler` antigo.

### Passo 6 — Remover `GeminiParser` e `Parser` interface (`packages/whatsapp/parser.go`)

- Remover `Parser` interface (linhas 44-46)
- Remover `GeminiParser` struct e `NewGeminiParser` (linhas 58-72)
- Remover `systemPrompt` / `buildSystemPrompt` (a Fase 2 tem prompt próprio no agente)
- Remover `geminiResponseSchema`
- Remover `geminiConfig`
- Remover `contentGenerator` interface e mover para `gemini_agent.go`
- Manter: `RegexParser`, `ParsedEntry`, `ParseAmount`, `humanCategory`, `ErrNotFinancial`

Nota: `ErrNotFinancial` não é mais usado pelo handler (o agente não retorna ele).
Pode ser mantido ou removido — se removido, verificar referências.

### Passo 7 — Wire (`apps/webhook/internal/app/app.go`)

```go
if apiKey := shared.Getenv("GEMINI_API_KEY", ""); apiKey != "" {
    agent, err := whatsapp.NewGeminiAgent(ctx, apiKey, store)
    if err != nil {
        log.Printf("NewFromEnv: gemini agent: %v, falling back to regex-only", err)
        finHandler = financial.NewHandler(whatsapp.NewRegexParser(), nil, store)
    } else {
        finHandler = financial.NewHandler(whatsapp.NewRegexParser(), agent, store)
        nlFinance = true
    }
} else {
    finHandler = financial.NewHandler(whatsapp.NewRegexParser(), nil, store)
}
```

### Passo 8 — Testes (`packages/whatsapp/gemini_agent_test.go` — novo)

Reaproveitar `fakeContentGenerator` do `gemini_parser_test.go`:

```go
// TestAgentCreateEntry: Gemini retorna FunctionCall → handler executa → resposta
// TestAgentMonthSummary: idem
// TestAgentChitChat: Gemini retorna texto → resposta direta
// TestAgentTooManyRounds: 5 function calls → erro
// TestAgentUnknownTool: função não registrada → erro
```

Os testes de tool usam `finance.NewInMemoryStore()` para evitar DynamoDB real.

### Passo 9 — Remover arquivos antigos

- `packages/whatsapp/gemini_parser_test.go`
- Referências a `GeminiParser` em `financial_handler_test.go` (se existir)

### Verificação

```bash
make test          # todos os testes passam
make build         # compila
make build-lambdas # zips para deploy
```

---

## Dependências entre fases

- **Fase 1** está implementada (PR #21, branch `gemini-refactor`).
- **Fase 2** substitui o `GeminiParser`. A Fase 1 (prompt dinâmico) é naturalmente
  absorvida pelo prompt do agente.

Ordem recomendada: ~~Fase 1~~ ✅ → testar em produção → Fase 2.
