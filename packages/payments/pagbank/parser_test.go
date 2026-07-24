package pagbank

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/emerson/emerbot/packages/payments"
)

// envelopeFromFixture builds an import envelope from PagBank's official test
// scenario JSONs (mirrored under testdata/), embedding each extract's raw API
// response object verbatim — the parser is exercised against real payloads.
func envelopeFromFixture(t *testing.T, scenario string) []byte {
	t.Helper()
	read := func(name string) json.RawMessage {
		b, err := os.ReadFile(filepath.Join("testdata", scenario, name))
		if err != nil {
			t.Fatalf("read fixture %s/%s: %v", scenario, name, err)
		}
		return b
	}
	raw, err := json.Marshal(struct {
		Provider      string          `json:"provider"`
		Date          string          `json:"date"`
		Transactional json.RawMessage `json:"transactional"`
		Financial     json.RawMessage `json:"financial"`
	}{"pagbank", "2026-07-23", read("transactional.json"), read("financial.json")})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return raw
}

func TestParseCreditInstallmentsFixture(t *testing.T) {
	got, err := New().Parse(envelopeFromFixture(t, "credit-installments"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Provider != payments.ProviderPagBank || got.SourceDate.String() != "2026-07-23" {
		t.Fatalf("header = %q/%q", got.Provider, got.SourceDate.String())
	}

	if len(got.Sales) != 1 {
		t.Fatalf("sales = %d, want 1", len(got.Sales))
	}
	s := got.Sales[0]
	wantID := payments.NewSaleID(payments.ProviderPagBank, "B140EE618E6E4428852EE4B474D05DB0")
	if s.ID != wantID {
		t.Errorf("sale id = %q, want %q", s.ID, wantID)
	}
	// gross 300.00, net 289.03 → fee 10.97.
	if s.GrossAmount != 30000 || s.NetAmount != 28903 || s.FeeAmount != 1097 {
		t.Errorf("amounts gross/net/fee = %d/%d/%d, want 30000/28903/1097", s.GrossAmount, s.NetAmount, s.FeeAmount)
	}
	if s.Method != payments.MethodCredito || s.Brand != "VISA" || s.Installments != 3 {
		t.Errorf("method/brand/installments = %q/%q/%d", s.Method, s.Brand, s.Installments)
	}

	if len(got.Receivables) != 1 {
		t.Fatalf("receivables = %d, want 1", len(got.Receivables))
	}
	r := got.Receivables[0]
	if r.Amount != 9634 || r.ExpectedDate.String() != "2024-05-09" || r.InstallmentNumber != 1 || r.InstallmentTotal != 3 {
		t.Errorf("receivable = %+v", r)
	}

	if len(got.Payments) != 1 {
		t.Fatalf("payments = %d, want 1", len(got.Payments))
	}
	if got.Payments[0].Amount != 9634 || got.Payments[0].PaymentDate.String() != "2024-05-09" || got.Payments[0].SaleID != wantID {
		t.Errorf("payment = %+v", got.Payments[0])
	}
}

func TestParseDebitFixture(t *testing.T) {
	got, err := New().Parse(envelopeFromFixture(t, "debit"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Sales) != 1 {
		t.Fatalf("sales = %d, want 1", len(got.Sales))
	}
	s := got.Sales[0]
	// gross 100.00, net 98.45 → fee 1.55; quantidade_parcelas "0" floors to 1.
	if s.GrossAmount != 10000 || s.NetAmount != 9845 || s.FeeAmount != 155 {
		t.Errorf("amounts gross/net/fee = %d/%d/%d, want 10000/9845/155", s.GrossAmount, s.NetAmount, s.FeeAmount)
	}
	if s.Method != payments.MethodDebito || s.Brand != "BANRICOMPRAS" || s.Installments != 1 {
		t.Errorf("method/brand/installments = %q/%q/%d", s.Method, s.Brand, s.Installments)
	}
	if len(got.Receivables) != 1 || got.Receivables[0].Amount != 9845 {
		t.Errorf("receivables = %+v", got.Receivables)
	}
	if len(got.Payments) != 1 || got.Payments[0].Amount != 9845 {
		t.Errorf("payments = %+v", got.Payments)
	}
}

func TestParseIsDeterministic(t *testing.T) {
	env := envelopeFromFixture(t, "credit-installments")
	a, err := New().Parse(env)
	if err != nil {
		t.Fatalf("Parse a: %v", err)
	}
	b, err := New().Parse(env)
	if err != nil {
		t.Fatalf("Parse b: %v", err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Error("Parse is not deterministic: two runs differ")
	}
}

// The official transactional response is paginated to one line, so the
// installment-dedup path (one sale, N receivables from N lines sharing
// codigo_transacao) and the skip-non-venda-event path are exercised with an
// inline múltiplo payload using the real field names and array-of-detalhe shape.
func TestParseMultiploDedupAndEventFilter(t *testing.T) {
	env := `{"provider":"pagbank","date":"2026-07-23",
	  "transactional":{"detalhes":[
	    {"tipo_evento":"1","codigo_transacao":"ABC","data_venda_ajuste":"2026-07-23","valor_total_transacao":108.96,"valor_liquido_transacao":105.00,"valor_parcela":35.00,"parcela":"1","quantidade_parcelas":"3","data_prevista_pagamento":"2026-08-23","meio_pagamento":"3","instituicao_financeira":"MASTERCARD"},
	    {"tipo_evento":"1","codigo_transacao":"ABC","data_venda_ajuste":"2026-07-23","valor_total_transacao":108.96,"valor_liquido_transacao":105.00,"valor_parcela":35.00,"parcela":"2","quantidade_parcelas":"3","data_prevista_pagamento":"2026-09-23","meio_pagamento":"3","instituicao_financeira":"MASTERCARD"},
	    {"tipo_evento":"1","codigo_transacao":"ABC","data_venda_ajuste":"2026-07-23","valor_total_transacao":108.96,"valor_liquido_transacao":105.00,"valor_parcela":35.00,"parcela":"3","quantidade_parcelas":"3","data_prevista_pagamento":"2026-10-23","meio_pagamento":"3","instituicao_financeira":"MASTERCARD"},
	    {"tipo_evento":"5","codigo_transacao":"CBK","data_venda_ajuste":"2026-07-23","valor_total_transacao":10.00,"valor_liquido_transacao":10.00,"valor_parcela":10.00,"parcela":"1","quantidade_parcelas":"1","data_prevista_pagamento":"2026-07-23","meio_pagamento":"3","instituicao_financeira":"VISA"}
	  ]},
	  "financial":{"detalhes":[
	    {"tipo_evento":"3","codigo_transacao":"ADJ","data_movimentacao":"2026-07-23","valor_parcela":1.00}
	  ]}}`

	got, err := New().Parse([]byte(env))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Sales) != 1 { // chargeback (event 5) skipped; ABC deduped
		t.Fatalf("sales = %d, want 1", len(got.Sales))
	}
	if len(got.Receivables) != 3 {
		t.Fatalf("receivables = %d, want 3", len(got.Receivables))
	}
	wantDates := []string{"2026-08-23", "2026-09-23", "2026-10-23"}
	for i, r := range got.Receivables {
		if r.ExpectedDate.String() != wantDates[i] || r.InstallmentNumber != i+1 {
			t.Errorf("receivable %d = %+v", i, r)
		}
	}
	if len(got.Payments) != 0 { // financial adjustment (event 3) skipped
		t.Fatalf("payments = %d, want 0", len(got.Payments))
	}
}

func TestMapMeioPagamentoAcceptsPaddedAndUnpadded(t *testing.T) {
	cases := map[string]payments.PaymentMethod{
		"3": payments.MethodCredito, "03": payments.MethodCredito,
		"8": payments.MethodDebito, "08": payments.MethodDebito,
		"1": payments.MethodDebito, "01": payments.MethodDebito,
		"11": payments.MethodPix, "2": payments.MethodBoleto, "02": payments.MethodBoleto,
		"99": payments.MethodOutros, "": payments.MethodOutros,
	}
	for code, want := range cases {
		if got := mapMeioPagamento(code); got != want {
			t.Errorf("mapMeioPagamento(%q) = %q, want %q", code, got, want)
		}
	}
}
