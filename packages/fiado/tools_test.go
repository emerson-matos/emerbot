package fiado

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The tools are what the pharmacy actually talks to, so these drive them the
// way the agent does: a JSON blob in, a map out. What they mostly assert is the
// refusals — a tool that writes when it should have asked is how one person
// becomes three debtors.

func toolset(t *testing.T) (map[string]Tool, Store) {
	t.Helper()
	store := NewInMemoryStore()
	byName := map[string]Tool{}
	for _, tool := range Tools(store, time.UTC) {
		byName[tool.Name] = tool
	}
	return byName, store
}

func call(t *testing.T, tool Tool, args map[string]any) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out, err := tool.Handler(context.Background(), user, raw)
	if err != nil {
		return nil, err
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("tool %s returned %T, want a map", tool.Name, out)
	}
	return m, nil
}

func mustCall(t *testing.T, tool Tool, args map[string]any) map[string]any {
	t.Helper()
	out, err := call(t, tool, args)
	if err != nil {
		t.Fatalf("tool %s: %v", tool.Name, err)
	}
	return out
}

func TestToolsDoNotTouchTheExistingOnes(t *testing.T) {
	tools, _ := toolset(t)
	// A caderninho tool must never shadow a ledger tool: the two systems are
	// separate and the model has to be able to reach both.
	for _, ledger := range []string{"create_financial_entry", "edit_financial_entry", "list_due_entries", "search_entries"} {
		if _, ok := tools[ledger]; ok {
			t.Fatalf("o caderninho declarou %q, que é ferramenta do razão", ledger)
		}
	}
	for _, want := range []string{
		registrarFiadoToolName, registrarPagamentoToolName, listarFiadosToolName,
		consultarFiadoToolName, fiadosDoDiaToolName, apagarMovimentoToolName,
	} {
		if _, ok := tools[want]; !ok {
			t.Fatalf("ferramenta %q não foi declarada", want)
		}
	}
}

func TestRegistrarFiadoWritesAndReportsTheBalance(t *testing.T) {
	tools, _ := toolset(t)

	got := mustCall(t, tools[registrarFiadoToolName], map[string]any{
		"cliente": "João Silva", "valor": 40.0, "data": "2026-08-01", "descricao": "dipirona",
	})
	if got["cliente"] != "joao_silva" || got["nome"] != "João Silva" {
		t.Fatalf("resultado = %+v", got)
	}
	if got["saldo_atual"] != 40.0 {
		t.Fatalf("saldo_atual = %v, want 40", got["saldo_atual"])
	}
	if got["situacao"] != "devendo" {
		t.Fatalf("situacao = %v, want devendo", got["situacao"])
	}
	if got["movimento_id"] == "" {
		t.Fatal("sem movimento_id: não haveria como apagar o movimento errado depois")
	}
}

// Fiado sem cliente é recusado, e o erro diz o que fazer: perguntar.
func TestRegistrarFiadoRefusesWithoutAClient(t *testing.T) {
	tools, _ := toolset(t)

	_, err := call(t, tools[registrarFiadoToolName], map[string]any{"valor": 40.0})
	if err == nil {
		t.Fatal("fiado sem cliente foi aceito")
	}
	if !strings.Contains(err.Error(), "pergunte") {
		t.Fatalf("err = %v, want it to tell the model to ask", err)
	}
}

func TestRegistrarFiadoRefusesANegativeAmount(t *testing.T) {
	tools, _ := toolset(t)

	_, err := call(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": -40.0})
	if err == nil {
		t.Fatal("venda fiado negativa foi aceita")
	}
	// And it points at the tool that does mean "the client paid".
	if !strings.Contains(err.Error(), registrarPagamentoToolName) {
		t.Fatalf("err = %v, want it to point at %s", err, registrarPagamentoToolName)
	}
}

// The reconciliation the feature does not work without: a name that looks like
// somebody already in the book is a question, not a second debtor.
func TestRegistrarFiadoAsksBeforeCreatingALookalike(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{
		"cliente": "João Silva", "valor": 300.0, "data": "2026-06-12",
	})

	_, err := call(t, tools[registrarFiadoToolName], map[string]any{
		"cliente": "joão", "valor": 40.0, "data": "2026-08-11",
	})
	if err == nil {
		t.Fatal("um segundo devedor foi criado sem perguntar")
	}
	// The error carries the candidate, its balance and the way out, so the model
	// can ask a question the user can answer.
	for _, want := range []string{"João Silva", "joao_silva", "R$ 300,00", "cliente_novo=true"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to mention %q", err, want)
		}
	}
	// And the aging is spoken the only way this feature speaks it.
	if strings.Contains(err.Error(), "vencid") || strings.Contains(err.Error(), "atrasad") {
		t.Fatalf("err = %v: fiado não vence, envelhece", err)
	}
	if !strings.Contains(err.Error(), "em aberto") {
		t.Fatalf("err = %v, want the aging in the caderninho's vocabulary", err)
	}

	// Nothing was written on the way to asking.
	book := mustCall(t, tools[listarFiadosToolName], map[string]any{})
	if book["count"] != 1 {
		t.Fatalf("caderninho tem %v linhas, want 1", book["count"])
	}
}

// Confirmed by the user, the same call goes through — the refusal is a question,
// not a wall.
func TestRegistrarFiadoCreatesALookalikeOnceConfirmed(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João Silva", "valor": 300.0})

	got := mustCall(t, tools[registrarFiadoToolName], map[string]any{
		"cliente": "João Pereira", "valor": 40.0, "cliente_novo": true,
	})
	if got["cliente"] != "joao_pereira" {
		t.Fatalf("cliente = %v, want joao_pereira", got["cliente"])
	}
}

// Somebody entirely new is not a question: that is just a new client.
func TestRegistrarFiadoDoesNotAskAboutAStranger(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João Silva", "valor": 300.0})

	got := mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Marcos", "valor": 20.0})
	if got["cliente"] != "marcos" {
		t.Fatalf("cliente = %v, want marcos", got["cliente"])
	}
}

// A payment from somebody who owes nothing is nearly always a misspelt name,
// and taking it would open an account in credit under the wrong person.
func TestRegistrarPagamentoRefusesSomebodyNotInTheBook(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Ana", "valor": 100.0})

	_, err := call(t, tools[registrarPagamentoToolName], map[string]any{"cliente": "Marcos", "valor": 50.0})
	if err == nil {
		t.Fatal("pagamento de quem não está no caderninho foi aceito")
	}
	if !strings.Contains(err.Error(), "Ana") {
		t.Fatalf("err = %v, want it to list who is in the book", err)
	}
}

func TestRegistrarPagamentoAbatesTheBalance(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": 70.0, "data": "2026-08-01"})

	got := mustCall(t, tools[registrarPagamentoToolName], map[string]any{
		"cliente": "João", "valor": 50.0, "data": "2026-08-05",
	})
	// The sign is chosen by the tool, never by the model.
	if got["valor"] != -50.0 {
		t.Fatalf("valor = %v, want -50", got["valor"])
	}
	if got["saldo_atual"] != 20.0 {
		t.Fatalf("saldo_atual = %v, want 20", got["saldo_atual"])
	}
}

func TestConsultarFiadoAnswersTheOwnersQuestions(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": 70.0, "data": "2026-08-01"})
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": 30.0, "data": "2026-08-03"})
	mustCall(t, tools[registrarPagamentoToolName], map[string]any{"cliente": "João", "valor": 50.0, "data": "2026-08-05"})

	got := mustCall(t, tools[consultarFiadoToolName], map[string]any{"cliente": "joão"})

	// "quanto o João me deve", "quanto o João me pagou", "desde quando".
	if got["saldo"] != 50.0 {
		t.Fatalf("saldo = %v, want 50", got["saldo"])
	}
	if got["total_pago"] != 50.0 {
		t.Fatalf("total_pago = %v, want 50 (a soma dos negativos)", got["total_pago"])
	}
	if got["total_comprado"] != 100.0 {
		t.Fatalf("total_comprado = %v, want 100", got["total_comprado"])
	}
	if got["desde"] != "2026-08-01" {
		t.Fatalf("desde = %v, want 2026-08-01", got["desde"])
	}
	movs, ok := got["movimentos"].([]map[string]any)
	if !ok || len(movs) != 3 {
		t.Fatalf("movimentos = %#v, want 3", got["movimentos"])
	}
	// Most recent first, and each one carries what apagar_movimento_fiado needs.
	if movs[0]["data"] != "2026-08-05" || movs[0]["tipo"] != "pagamento" {
		t.Fatalf("primeiro movimento = %+v", movs[0])
	}
	if movs[0]["movimento_id"] == "" {
		t.Fatal("movimento sem id: não daria para corrigir um erro")
	}
}

func TestConsultarFiadoAsksAboutALookalike(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João Silva", "valor": 300.0})

	_, err := call(t, tools[consultarFiadoToolName], map[string]any{"cliente": "joão"})
	if err == nil {
		t.Fatal("consulta a um nome parecido respondeu como se a pessoa não existisse")
	}
	if !strings.Contains(err.Error(), "joao_silva") {
		t.Fatalf("err = %v, want the candidate", err)
	}
}

func TestFiadosDoDia(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": 70.0, "data": "2026-08-01"})
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Ana", "valor": 30.0, "data": "2026-08-02"})
	mustCall(t, tools[registrarPagamentoToolName], map[string]any{"cliente": "João", "valor": 20.0, "data": "2026-08-02"})

	got := mustCall(t, tools[fiadosDoDiaToolName], map[string]any{"data": "2026-08-02"})
	if got["count"] != 2 {
		t.Fatalf("count = %v, want 2", got["count"])
	}
	if got["total_fiado"] != 30.0 || got["total_recebido"] != 20.0 {
		t.Fatalf("totais = %v/%v, want 30/20", got["total_fiado"], got["total_recebido"])
	}
}

func TestListarFiadosPutsTheBiggestDebtFirstAndTotalsEverything(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Ana", "valor": 10.0})
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Zeca", "valor": 90.0})
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Bia", "valor": 50.0})

	got := mustCall(t, tools[listarFiadosToolName], map[string]any{})
	if got["total_em_aberto"] != 150.0 {
		t.Fatalf("total_em_aberto = %v, want 150", got["total_em_aberto"])
	}
	list := got["devedores"].([]map[string]any)
	order := []string{list[0]["cliente"].(string), list[1]["cliente"].(string), list[2]["cliente"].(string)}
	if strings.Join(order, ",") != "zeca,bia,ana" {
		t.Fatalf("ordem = %v, want zeca,bia,ana", order)
	}
}

// Someone who settled up is out of the list by default — the question is who
// owes — but still findable, because the name reconciliation needs them.
func TestListarFiadosHidesSettledAccountsUnlessAsked(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Ana", "valor": 10.0})
	mustCall(t, tools[registrarPagamentoToolName], map[string]any{"cliente": "Ana", "valor": 10.0})
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "Zeca", "valor": 90.0})

	got := mustCall(t, tools[listarFiadosToolName], map[string]any{})
	if got["count"] != 1 {
		t.Fatalf("count = %v, want 1", got["count"])
	}
	withSettled := mustCall(t, tools[listarFiadosToolName], map[string]any{"incluir_quitados": true})
	if withSettled["count"] != 2 {
		t.Fatalf("count com quitados = %v, want 2", withSettled["count"])
	}
}

// A cut list never goes out quietly, and the totals it goes out with cover
// everything (ADR-015).
func TestListarFiadosWarnsWhenItCuts(t *testing.T) {
	tools, _ := toolset(t)
	for i := range maxDebtorsInTool + 5 {
		mustCall(t, tools[registrarFiadoToolName], map[string]any{
			// cliente_novo because the generated names are deliberately
			// similar; the reconciliation has its own tests above.
			"cliente": nameFor(i), "valor": float64(i + 1), "cliente_novo": true,
		})
	}

	got := mustCall(t, tools[listarFiadosToolName], map[string]any{})
	if got["truncated"] != true {
		t.Fatalf("truncated = %v, want true", got["truncated"])
	}
	if got["count"] != maxDebtorsInTool {
		t.Fatalf("count = %v, want %d", got["count"], maxDebtorsInTool)
	}
	warning, _ := got["warning"].(string)
	if warning == "" || !strings.Contains(warning, "total_em_aberto") {
		t.Fatalf("warning = %q, want it to point at the total that is complete", warning)
	}
	// The total is over everybody: 1+2+…+55 reais.
	n := float64(maxDebtorsInTool + 5)
	if got["total_em_aberto"] != n*(n+1)/2 {
		t.Fatalf("total_em_aberto = %v, want %v — o total nunca é parcial", got["total_em_aberto"], n*(n+1)/2)
	}
}

// Correcting a mistake deletes the wrong line and gives the balance back.
func TestApagarMovimentoRestoresTheBalance(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": 40.0, "data": "2026-08-01"})
	wrong := mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": 99.0, "data": "2026-08-02"})

	got := mustCall(t, tools[apagarMovimentoToolName], map[string]any{
		"cliente": "joao", "data": "2026-08-02", "movimento_id": wrong["movimento_id"],
	})
	if got["saldo_atual"] != 40.0 {
		t.Fatalf("saldo_atual = %v, want 40", got["saldo_atual"])
	}

	after := mustCall(t, tools[consultarFiadoToolName], map[string]any{"cliente": "João"})
	if after["total_comprado"] != 40.0 {
		t.Fatalf("total_comprado = %v, want 40 — o movimento apagado ainda conta", after["total_comprado"])
	}
	if len(after["movimentos"].([]map[string]any)) != 1 {
		t.Fatalf("movimentos = %v, want 1", after["movimentos"])
	}
}

func TestApagarMovimentoRefusesAnUnknownOne(t *testing.T) {
	tools, _ := toolset(t)
	mustCall(t, tools[registrarFiadoToolName], map[string]any{"cliente": "João", "valor": 40.0, "data": "2026-08-01"})

	_, err := call(t, tools[apagarMovimentoToolName], map[string]any{
		"cliente": "joao", "data": "2026-08-01", "movimento_id": "nao-existe",
	})
	if err == nil {
		t.Fatal("apagar um movimento inexistente respondeu sucesso")
	}
}

// nameFor makes distinct clients whose slugs cannot be mistaken for each
// other's abbreviations.
func nameFor(i int) string {
	letters := []string{"alfa", "beta", "gama", "delta", "epsilon", "zeta", "eta", "teta", "iota", "kapa"}
	return letters[i%len(letters)] + strings.Repeat("x", i/len(letters)+1) + string(rune('a'+i%26))
}

// The candidate line is where the caderninho's vocabulary reaches the model, so
// it is asserted against a fixed day rather than the wall clock.
func TestDescribeDebtorSpeaksTheCaderninhosVocabulary(t *testing.T) {
	since := day(t, "2026-06-12")
	today := day(t, "2026-08-11")
	cases := map[string]struct {
		debtor Debtor
		want   string
	}{
		"devendo": {
			Debtor{Name: "João Silva", Client: "joao_silva", Balance: 30000, Since: &since},
			"João Silva (joao_silva), R$ 300,00 em aberto há 60 dias",
		},
		"quite":   {Debtor{Name: "Ana", Client: "ana"}, "Ana (ana), R$ 0,00 (quite)"},
		"credito": {Debtor{Name: "Bia", Client: "bia", Balance: -500}, "Bia (bia), R$ 5,00 de crédito"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			got := describeDebtor(tc.debtor, today)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for _, forbidden := range []string{"vencid", "atrasad", "inadimpl"} {
				if strings.Contains(got, forbidden) {
					t.Fatalf("%q usa %q — fiado não vence, envelhece", got, forbidden)
				}
			}
		})
	}
}
