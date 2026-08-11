package fiado

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/emerson/emerbot/packages/domain"
)

// The caderninho's own tools. No existing tool changes: create_financial_entry
// and edit_financial_entry are not touched, and list_due_entries has nothing to
// do with fiado — that one is about falling due, and fiado does not (ADR-027
// §6).

// ToolFunc executes one function call. args is the raw JSON the model produced.
type ToolFunc func(ctx context.Context, userID string, args json.RawMessage) (any, error)

// Tool bundles a function-call declaration with its handler.
//
// It mirrors finance.Tool field for field, and is declared here rather than
// imported from there on purpose: the caderninho does not depend on the ledger,
// and a type import would be the first thread pulling the two together.
// packages/orchestrator/internal/agenttools already knows both and is where
// they meet.
type Tool struct {
	Name        string
	Description string
	Parameters  *genai.Schema
	Handler     ToolFunc
}

// Tools builds the caderninho's tool set. loc is the calendar "hoje" resolves
// in — the pharmacy's, not the Lambda's UTC.
func Tools(store Store, loc *time.Location) []Tool {
	if loc == nil {
		loc = time.UTC
	}
	return []Tool{
		registrarFiadoTool(store, loc),
		registrarPagamentoTool(store, loc),
		listarFiadosTool(store, loc),
		consultarFiadoTool(store, loc),
		fiadosDoDiaTool(store, loc),
		apagarMovimentoTool(store),
	}
}

const (
	registrarFiadoToolName     = "registrar_fiado"
	registrarPagamentoToolName = "registrar_pagamento_fiado"
	listarFiadosToolName       = "listar_fiados"
	consultarFiadoToolName     = "consultar_fiado"
	fiadosDoDiaToolName        = "fiados_do_dia"
	apagarMovimentoToolName    = "apagar_movimento_fiado"
)

// maxDebtorsInTool and maxMovementsInTool bound what one tool result hands the
// model. Both are far above a counter's real volume, and when either fires the
// totals still cover everything — only the detail is cut, and it says so
// (ADR-015).
const (
	maxDebtorsInTool   = 50
	maxMovementsInTool = 30
)

const clienteArgDescription = "Nome do cliente como o usuário falou (ex: \"João Silva\"). " +
	"Obrigatório: fiado sem cliente é recusado, e \"fiado de quem?\" é uma pergunta de uma palavra. " +
	"Se o nome for parecido com alguém que já está no caderninho, a ferramenta devolve os " +
	"candidatos em vez de gravar — pergunte ao usuário qual é antes de insistir."

const dataArgDescription = "Data do movimento YYYY-MM-DD (padrão: hoje)"

// --- registrar_fiado ---

func registrarFiadoTool(store Store, loc *time.Location) Tool {
	return Tool{
		Name: registrarFiadoToolName,
		Description: "Registra uma venda fiado no caderninho: o cliente levou e ficou devendo. " +
			"NÃO é lançamento financeiro — venda fiado não é faturamento, não é caixa e não " +
			"cria conta a receber. Nunca chame create_financial_entry para isto.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"cliente":   {Type: genai.TypeString, Description: clienteArgDescription},
				"valor":     {Type: genai.TypeNumber, Description: "Valor em reais que o cliente levou (positivo, ex: 40.00)"},
				"data":      {Type: genai.TypeString, Description: dataArgDescription},
				"descricao": {Type: genai.TypeString, Description: "O que ele levou, se o usuário disser (ex: \"dipirona e fralda\")"},
				"cliente_novo": {
					Type: genai.TypeBoolean,
					Description: "Só use true depois de o usuário confirmar que é uma pessoa nova, " +
						"quando a ferramenta tiver devolvido candidatos parecidos. Nunca por conta própria.",
				},
			},
			Required: []string{"cliente", "valor"},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Cliente     string  `json:"cliente"`
				Valor       float64 `json:"valor"`
				Data        string  `json:"data"`
				Descricao   string  `json:"descricao"`
				ClienteNovo bool    `json:"cliente_novo"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if args.Valor <= 0 {
				return nil, fmt.Errorf("valor de uma venda fiado é positivo (o cliente levou): recebi %v. Para dar baixa, use %s", args.Valor, registrarPagamentoToolName)
			}
			return recordMovement(ctx, store, loc, userID, args.Cliente, reaisToCentavos(args.Valor), args.Data, args.Descricao, args.ClienteNovo, false)
		},
	}
}

// --- registrar_pagamento_fiado ---

func registrarPagamentoTool(store Store, loc *time.Location) Tool {
	return Tool{
		Name: registrarPagamentoToolName,
		Description: "Dá baixa no caderninho: o cliente pagou parte ou tudo do que devia. " +
			"O pagamento abate o saldo da pessoa e não abate nenhuma compra específica — " +
			"não tente escolher qual. NÃO cria lançamento de caixa: se o dinheiro também " +
			"tem que aparecer no razão, isso é um lançamento separado, que o usuário pede à parte.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"cliente":   {Type: genai.TypeString, Description: clienteArgDescription},
				"valor":     {Type: genai.TypeNumber, Description: "Valor em reais que o cliente pagou (positivo, ex: 50.00)"},
				"data":      {Type: genai.TypeString, Description: dataArgDescription},
				"descricao": {Type: genai.TypeString, Description: "Observação, se houver"},
				"cliente_novo": {
					Type: genai.TypeBoolean,
					Description: "Só use true se o usuário confirmar que é essa pessoa mesmo, " +
						"depois de a ferramenta ter recusado por não achá-la no caderninho.",
				},
			},
			Required: []string{"cliente", "valor"},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Cliente     string  `json:"cliente"`
				Valor       float64 `json:"valor"`
				Data        string  `json:"data"`
				Descricao   string  `json:"descricao"`
				ClienteNovo bool    `json:"cliente_novo"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			if args.Valor <= 0 {
				return nil, fmt.Errorf("informe quanto o cliente pagou, em reais e positivo: recebi %v", args.Valor)
			}
			// Stored negative: the sign is the only type there is, and it is the
			// tool that chooses it — never the model.
			return recordMovement(ctx, store, loc, userID, args.Cliente, -reaisToCentavos(args.Valor), args.Data, args.Descricao, args.ClienteNovo, true)
		},
	}
}

// recordMovement is the shared path of both writing tools: reconcile the name,
// then write. payment only changes how a stranger is treated — see below.
func recordMovement(
	ctx context.Context, store Store, loc *time.Location,
	userID, name string, amount int64, date, description string, confirmedNew, payment bool,
) (any, error) {
	slug := ClientSlug(name)
	if slug == "" {
		return nil, fmt.Errorf("%w: pergunte ao usuário de quem é esse fiado antes de registrar", ErrNoClient)
	}

	book, err := store.ListDebtors(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("consultar caderninho: %w", err)
	}
	today := domain.NewCalendarDate(time.Now().In(loc))

	if !inBook(book, slug) {
		// The whole reason the caderninho works: a name that is not in the book
		// but looks like one that is does not create a second person — it comes
		// back as a question, the same shape as an unknown category (ADR-024).
		if candidates := SimilarClients(slug, book); len(candidates) > 0 && !confirmedNew {
			return nil, errSimilarClients(name, candidates, today)
		}
		// A payment from somebody who owes nothing is almost always a misspelt
		// name, and it would open an account in credit under it. A *purchase*
		// from somebody new is just a new client.
		if payment && !confirmedNew {
			return nil, errUnknownDebtor(name, book)
		}
	}

	when := today
	if d, ok := parseDay(date); ok {
		when = d
	}
	m, err := NewMovement(userID, name, amount, when, description)
	if err != nil {
		return nil, err
	}
	debtor, err := store.Record(ctx, m)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status":         "registrado",
		"movimento_id":   m.ID,
		"cliente":        debtor.Client,
		"nome":           debtor.Name,
		"valor":          centavosToReais(m.Amount),
		"data":           m.Date.String(),
		"saldo_atual":    centavosToReais(debtor.Balance),
		"situacao":       accountState(debtor),
		"desde":          sinceString(debtor),
		"dias_em_aberto": DaysOpen(debtor, today),
	}, nil
}

// --- listar_fiados ---

func listarFiadosTool(store Store, loc *time.Location) Tool {
	return Tool{
		Name: listarFiadosToolName,
		Description: "Lista o caderninho: quem está devendo, quanto, e há quantos dias. " +
			"Use para \"como estão meus fiados\" e para conferir o nome de um cliente antes de registrar.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"incluir_quitados": {
					Type:        genai.TypeBoolean,
					Description: "Inclui quem já está quite ou com crédito (padrão: false, só quem deve)",
				},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				IncluirQuitados bool `json:"incluir_quitados"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, fmt.Errorf("parse args: %w", err)
				}
			}

			book, err := store.ListDebtors(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("consultar caderninho: %w", err)
			}
			today := domain.NewCalendarDate(time.Now().In(loc))

			var total int64
			kept := make([]Debtor, 0, len(book))
			for _, d := range book {
				if d.Balance > 0 {
					total += d.Balance
				}
				if d.Balance == 0 && !args.IncluirQuitados {
					continue
				}
				kept = append(kept, d)
			}
			// Biggest debt first, so a cut list cuts the ones that matter least.
			sortDebtorsByBalance(kept)

			shown := kept
			truncated := len(kept) > maxDebtorsInTool
			if truncated {
				shown = kept[:maxDebtorsInTool]
			}

			result := map[string]any{
				"devedores":       debtorsPayload(shown, today),
				"count":           len(shown),
				"total_clientes":  len(kept),
				"total_em_aberto": centavosToReais(total),
				"truncated":       truncated,
			}
			if truncated {
				// The total above covers everybody; only the list is short.
				result["warning"] = fmt.Sprintf(
					"Mostrando os %d maiores de %d clientes. total_em_aberto cobre TODOS — "+
						"use esse número e avise que a lista está parcial.",
					len(shown), len(kept),
				)
				log.Printf("fiado tool %s: truncated for user %s: %d of %d", listarFiadosToolName, userID, len(shown), len(kept))
			}
			return result, nil
		},
	}
}

// --- consultar_fiado ---

func consultarFiadoTool(store Store, loc *time.Location) Tool {
	return Tool{
		Name: consultarFiadoToolName,
		Description: "Consulta um cliente do caderninho: quanto ele deve agora, desde quando, " +
			"quanto já levou, quanto já pagou, e os últimos movimentos. " +
			"Responde \"quanto o João me deve\", \"quanto o João me pagou\" e " +
			"\"desde quando o João me deve\" — nunca some os movimentos você mesmo, os totais já vêm prontos.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"cliente": {Type: genai.TypeString, Description: "Nome do cliente como o usuário falou"},
			},
			Required: []string{"cliente"},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Cliente string `json:"cliente"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			slug := ClientSlug(args.Cliente)
			if slug == "" {
				return nil, fmt.Errorf("%w: de quem é a consulta?", ErrNoClient)
			}

			today := domain.NewCalendarDate(time.Now().In(loc))
			debtor, err := store.Debtor(ctx, userID, slug)
			if errors.Is(err, ErrDebtorNotFound) {
				book, listErr := store.ListDebtors(ctx, userID)
				if listErr != nil {
					return nil, err
				}
				if candidates := SimilarClients(slug, book); len(candidates) > 0 {
					return nil, errSimilarClients(args.Cliente, candidates, today)
				}
				return nil, errUnknownDebtor(args.Cliente, book)
			}
			if err != nil {
				return nil, err
			}

			// The whole history, because the two totals below are totals: a sum
			// read halfway is a wrong number with nothing to show it is one.
			page, err := store.ClientMovements(ctx, userID, slug, Page{})
			if err != nil {
				return nil, err
			}
			taken, paid := Totals(page.Movements)

			shown := page.Movements
			truncated := len(shown) > maxMovementsInTool
			if truncated {
				shown = shown[:maxMovementsInTool]
			}

			result := map[string]any{
				"cliente":          debtor.Client,
				"nome":             debtor.Name,
				"saldo":            centavosToReais(debtor.Balance),
				"situacao":         accountState(debtor),
				"desde":            sinceString(debtor),
				"dias_em_aberto":   DaysOpen(debtor, today),
				"total_comprado":   centavosToReais(taken),
				"total_pago":       centavosToReais(paid),
				"movimentos":       movementsPayload(shown),
				"total_movimentos": len(page.Movements),
				"truncated":        truncated,
			}
			if truncated {
				result["warning"] = fmt.Sprintf(
					"Mostrando os %d movimentos mais recentes de %d. saldo, total_comprado e "+
						"total_pago cobrem TODOS — avise que o detalhamento está parcial.",
					len(shown), len(page.Movements),
				)
				log.Printf("fiado tool %s: truncated for user %s: %d of %d", consultarFiadoToolName, userID, len(shown), len(page.Movements))
			}
			return result, nil
		},
	}
}

// --- fiados_do_dia ---

func fiadosDoDiaTool(store Store, loc *time.Location) Tool {
	return Tool{
		Name: fiadosDoDiaToolName,
		Description: "Mostra os movimentos do caderninho em um dia: quem levou fiado e quem pagou. " +
			"Responde \"como foram meus fiados hoje/no dia X\".",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"data": {Type: genai.TypeString, Description: "Dia YYYY-MM-DD (padrão: hoje)"},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Data string `json:"data"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, fmt.Errorf("parse args: %w", err)
				}
			}
			when := domain.NewCalendarDate(time.Now().In(loc))
			if d, ok := parseDay(args.Data); ok {
				when = d
			}

			page, err := store.DayMovements(ctx, userID, when, Page{})
			if err != nil {
				return nil, err
			}
			taken, paid := Totals(page.Movements)

			shown := page.Movements
			truncated := len(shown) > maxMovementsInTool
			if truncated {
				shown = shown[:maxMovementsInTool]
			}

			result := map[string]any{
				"data":             when.String(),
				"movimentos":       movementsPayload(shown),
				"count":            len(shown),
				"total_movimentos": len(page.Movements),
				"total_fiado":      centavosToReais(taken),
				"total_recebido":   centavosToReais(paid),
				"truncated":        truncated,
			}
			if truncated {
				result["warning"] = fmt.Sprintf(
					"Mostrando %d de %d movimentos do dia. total_fiado e total_recebido cobrem "+
						"TODOS — avise que a lista está parcial.",
					len(shown), len(page.Movements),
				)
				log.Printf("fiado tool %s: truncated for user %s: %d of %d", fiadosDoDiaToolName, userID, len(shown), len(page.Movements))
			}
			return result, nil
		},
	}
}

// --- apagar_movimento_fiado ---

func apagarMovimentoTool(store Store) Tool {
	return Tool{
		Name: apagarMovimentoToolName,
		Description: "Apaga um movimento errado do caderninho e devolve o saldo ao que era. " +
			"É assim que se corrige um erro — nunca lance um valor ao contrário para compensar: " +
			"um estorno registrado como pagamento entra em \"quanto o cliente pagou\" e conta " +
			"dinheiro que nunca existiu. Depois de apagar, registre o movimento certo, se houver. " +
			"Pegue movimento_id, cliente e data em " + consultarFiadoToolName + " ou " + fiadosDoDiaToolName + ".",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"cliente":      {Type: genai.TypeString, Description: "Slug do cliente, como veio na consulta"},
				"data":         {Type: genai.TypeString, Description: "Data do movimento YYYY-MM-DD, como veio na consulta"},
				"movimento_id": {Type: genai.TypeString, Description: "ID do movimento, como veio na consulta"},
			},
			Required: []string{"cliente", "data", "movimento_id"},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Cliente     string `json:"cliente"`
				Data        string `json:"data"`
				MovimentoID string `json:"movimento_id"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return nil, fmt.Errorf("parse args: %w", err)
			}
			date, ok := parseDay(args.Data)
			if !ok {
				return nil, fmt.Errorf("data %q não é uma data YYYY-MM-DD; use a que veio na consulta", args.Data)
			}
			// The slug as stored: the model is handed it by a listing, but it
			// sometimes echoes the display name instead.
			ref := Ref{Client: ClientSlug(args.Cliente), Date: date, ID: args.MovimentoID}

			debtor, err := store.Delete(ctx, userID, ref)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"status":      "apagado",
				"cliente":     debtor.Client,
				"nome":        debtor.Name,
				"saldo_atual": centavosToReais(debtor.Balance),
				"situacao":    accountState(debtor),
				"desde":       sinceString(debtor),
			}, nil
		},
	}
}

// --- shared helpers ---

// errSimilarClients refuses to invent a second person and hands the model the
// question to ask, with the balances that make the answer obvious to the user.
//
// Same shape as packages/finance's errUnknownCategory: the error teaches the
// way out. Without it "joão", "João Silva" and "Joao S." become three debtors,
// and "quanto o João me deve" answers with a third of what he owes — the
// caderninho lying for less is what makes somebody stop using it.
func errSimilarClients(typed string, candidates []Debtor, today domain.CalendarDate) error {
	parts := make([]string, 0, len(candidates))
	for _, c := range candidates {
		parts = append(parts, describeDebtor(c, today))
	}
	return fmt.Errorf(
		"%q não está no caderninho, mas tem gente parecida: %s. NÃO registre: pergunte ao "+
			"usuário de quem se trata e chame de novo com o nome que ele confirmar. "+
			"Se ele disser que é uma pessoa nova mesmo, chame de novo com cliente_novo=true",
		typed, strings.Join(parts, "; "),
	)
}

// errUnknownDebtor refuses a payment from somebody who is not in the book.
func errUnknownDebtor(typed string, book []Debtor) error {
	if len(book) == 0 {
		return fmt.Errorf(
			"%q não está no caderninho, que está vazio. Confirme com o usuário de quem é "+
				"antes de registrar; se for uma pessoa nova mesmo, chame de novo com cliente_novo=true",
			typed,
		)
	}
	names := make([]string, 0, len(book))
	for _, d := range book {
		names = append(names, d.Name)
	}
	return fmt.Errorf(
		"%q não está no caderninho. Quem está: %s. Confirme com o usuário de quem se trata; "+
			"se for mesmo essa pessoa, chame de novo com cliente_novo=true",
		typed, strings.Join(names, ", "),
	)
}

// describeDebtor is one candidate, in the caderninho's vocabulary: a debt is
// "em aberto há N dias", never overdue.
func describeDebtor(d Debtor, today domain.CalendarDate) string {
	line := fmt.Sprintf("%s (%s), %s", d.Name, d.Client, formatReais(d.Balance))
	switch {
	case d.Balance < 0:
		return line + " de crédito"
	case d.Balance == 0:
		return line + " (quite)"
	}
	if days := DaysOpen(d, today); days != nil {
		return fmt.Sprintf("%s em aberto há %d dias", line, *days)
	}
	return line + " em aberto"
}

func inBook(book []Debtor, slug string) bool {
	for _, d := range book {
		if d.Client == slug {
			return true
		}
	}
	return false
}

// sortDebtorsByBalance puts the biggest debt first, so a list that has to be
// cut cuts the people who matter least. The slug breaks ties, because two
// clients owing the same amount must not come back in a different order each
// call.
func sortDebtorsByBalance(debtors []Debtor) {
	sort.Slice(debtors, func(i, j int) bool {
		if debtors[i].Balance != debtors[j].Balance {
			return debtors[i].Balance > debtors[j].Balance
		}
		return debtors[i].Client < debtors[j].Client
	})
}

func debtorsPayload(debtors []Debtor, today domain.CalendarDate) []map[string]any {
	out := make([]map[string]any, 0, len(debtors))
	for _, d := range debtors {
		out = append(out, map[string]any{
			"cliente":        d.Client,
			"nome":           d.Name,
			"saldo":          centavosToReais(d.Balance),
			"situacao":       accountState(d),
			"desde":          sinceString(d),
			"dias_em_aberto": DaysOpen(d, today),
		})
	}
	return out
}

func movementsPayload(movements []Movement) []map[string]any {
	out := make([]map[string]any, 0, len(movements))
	for _, m := range movements {
		out = append(out, map[string]any{
			"movimento_id": m.ID,
			"cliente":      m.Client,
			"nome":         m.Name,
			// Signed, because the sign is the type: positive is what they took,
			// negative is what they paid.
			"valor":     centavosToReais(m.Amount),
			"tipo":      movementLabel(m.Amount),
			"data":      m.Date.String(),
			"descricao": m.Description,
		})
	}
	return out
}

// movementLabel names the sign for the model. It is a rendering of the sign,
// not a stored field — nothing may start filtering on it.
func movementLabel(amount int64) string {
	if amount < 0 {
		return "pagamento"
	}
	return "fiado"
}

// accountState names the three things a balance can be, so a reply never calls
// a credit a debt.
func accountState(d Debtor) string {
	switch {
	case d.Balance > 0:
		return "devendo"
	case d.Balance < 0:
		return "credito"
	default:
		return "quite"
	}
}

func sinceString(d Debtor) any {
	if d.Since == nil {
		return nil
	}
	return d.Since.String()
}

// parseDay reads a tool's date argument. Tool args come from LLM output, where
// "no date given" and "a date I could not read" both mean "use today" — which
// is why this reports a bool rather than an error, the same as the finance
// tools do.
func parseDay(s string) (domain.CalendarDate, bool) {
	if s == "" {
		return domain.CalendarDate{}, false
	}
	d, err := domain.ParseCalendarDate(s)
	if err != nil {
		return domain.CalendarDate{}, false
	}
	return d, true
}

func reaisToCentavos(reais float64) int64 {
	if reais < 0 {
		return -int64(-reais*100 + 0.5)
	}
	return int64(reais*100 + 0.5)
}

func centavosToReais(centavos int64) float64 { return float64(centavos) / 100 }

// formatReais writes an amount the way it is spoken in pt-BR, for the error
// messages the model reads back to the user.
func formatReais(centavos int64) string {
	if centavos < 0 {
		centavos = -centavos
	}
	return "R$ " + strings.Replace(fmt.Sprintf("%.2f", centavosToReais(centavos)), ".", ",", 1)
}
