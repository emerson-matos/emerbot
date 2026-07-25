package pagbank

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emerson/emerbot/packages/payments"
)

func extract(kind ExtractKind, body string) Extract {
	return Extract{Kind: kind, Name: string(kind) + ".json", Data: []byte(body)}
}

func decodeEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, raw)
	}
	return env
}

func TestClassifyFile(t *testing.T) {
	cases := map[string]ExtractKind{
		"cenario1_response_transactional_1.json": KindTransactional,
		"cenario1_response_financial_2.json":     KindFinancial,
		"cenario1_response_cashouts3.json":       KindCashouts,
		"cenario3_response_cashouts_2.json":      KindCashouts,
	}
	for name, want := range cases {
		got, ok := ClassifyFile(name)
		if !ok || got != want {
			t.Errorf("ClassifyFile(%q) = %q,%v; want %q,true", name, got, ok, want)
		}
	}
	if _, ok := ClassifyFile("README.md"); ok {
		t.Error("ClassifyFile should not claim an unrelated file")
	}
}

// The source date drives the replace unit, so deriving it must be deterministic:
// the newest business date in the payload, not the wall clock.
func TestAssembleDerivesSourceDateFromNewestRecord(t *testing.T) {
	raw, err := Assemble([]Extract{
		extract(KindTransactional, `{"detalhes":[
		  {"tipo_evento":"1","data_venda_ajuste":"2026-07-20","data_prevista_pagamento":"2026-08-20"}
		]}`),
		extract(KindFinancial, `{"detalhes":[
		  {"tipo_evento":"1","data_movimentacao":"2026-07-23"}
		]}`),
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if got := decodeEnvelope(t, raw)["date"]; got != "2026-08-20" {
		t.Errorf("date = %v, want 2026-08-20 (the newest date present)", got)
	}
}

func TestAssembleMergesMultipleFilesOfOneKind(t *testing.T) {
	raw, err := Assemble([]Extract{
		extract(KindFinancial, `{"detalhes":[{"codigo_transacao":"A","data_movimentacao":"2026-07-01"}]}`),
		extract(KindFinancial, `{"detalhes":[{"codigo_transacao":"B","data_movimentacao":"2026-07-02"}]}`),
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	env := decodeEnvelope(t, raw)
	fin, _ := env["financial"].(map[string]any)
	list, _ := fin["detalhes"].([]any)
	if len(list) != 2 {
		t.Errorf("financial detalhes = %d, want 2 (both files merged)", len(list))
	}
}

// Rebasing must move every date as a block, by whole months, so a monthly
// installment plan still reads as one. Shifting each date independently, or by a
// fixed day count, would scramble the plan the forecast displays.
func TestAssembleRebaseShiftsByWholeMonths(t *testing.T) {
	raw, err := Assemble([]Extract{
		extract(KindTransactional, `{"detalhes":[
		  {"data_venda_ajuste":"2024-07-30","data_prevista_pagamento":"2024-08-30"},
		  {"data_venda_ajuste":"2024-07-30","data_prevista_pagamento":"2024-09-30"}
		]}`),
	}, AssembleOptions{RebaseTo: "2026-03"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	env := decodeEnvelope(t, raw)
	tr, _ := env["transactional"].(map[string]any)
	list, _ := tr["detalhes"].([]any)
	if len(list) != 2 {
		t.Fatalf("detalhes = %d, want 2", len(list))
	}
	first, _ := list[0].(map[string]any)
	second, _ := list[1].(map[string]any)

	// Newest date 2024-09-30 → 2026-03, i.e. +18 months applied to everything.
	if got := first["data_venda_ajuste"]; got != "2026-01-30" {
		t.Errorf("sale date = %v, want 2026-01-30", got)
	}
	// 2024-08-30 + 18 months is 2026-02-30, which does not exist: clamp to the
	// last day of February rather than overflowing into March, which would put
	// installment 1 after installment 2.
	if got := first["data_prevista_pagamento"]; got != "2026-02-28" {
		t.Errorf("installment 1 = %v, want 2026-02-28 (clamped, not overflowed)", got)
	}
	if got := second["data_prevista_pagamento"]; got != "2026-03-30" {
		t.Errorf("installment 2 = %v, want 2026-03-30", got)
	}
}

// Amounts must survive assembly byte for byte — a round-trip through float64
// could turn 108.96 into 108.95999999999999 and silently move money.
func TestAssemblePreservesAmountLiterals(t *testing.T) {
	raw, err := Assemble([]Extract{
		extract(KindTransactional, `{"detalhes":[
		  {"data_venda_ajuste":"2026-07-20","valor_total_transacao":108.96,"tarifa_intermediacao":0.0}
		]}`),
	}, AssembleOptions{})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	body := string(raw)
	for _, literal := range []string{"108.96", "0.0"} {
		if !strings.Contains(body, literal) {
			t.Errorf("envelope lost the literal %q:\n%s", literal, body)
		}
	}
}

func TestAssembleRejectsUndatedPayload(t *testing.T) {
	_, err := Assemble([]Extract{
		extract(KindTransactional, `{"detalhes":[{"codigo_transacao":"A"}]}`),
	}, AssembleOptions{})
	if err == nil {
		t.Error("expected an error when no date is present and none was given")
	}
}

// The recorded PagBank scenarios are the closest thing to real input we have.
// Assembling and then parsing each one is the end-to-end check that the two
// halves of this package agree, and that the fixtures stay importable.
func TestAssembleAndParseEveryRecordedScenario(t *testing.T) {
	root := filepath.Join("..", "..", "..", "cenarios")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("recorded scenarios not present: %v", err)
	}

	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		t.Run(dir.Name(), func(t *testing.T) {
			extracts := readScenario(t, filepath.Join(root, dir.Name()))
			if len(extracts) == 0 {
				t.Fatal("no EDI responses found")
			}

			raw, err := Assemble(extracts, AssembleOptions{})
			if err != nil {
				t.Fatalf("Assemble: %v", err)
			}

			result, err := New().Parse(raw)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if err := payments.ValidateImportResult(result); err != nil {
				t.Fatalf("assembled scenario is not importable: %v", err)
			}
			if len(result.Sales) == 0 && len(result.Payments) == 0 {
				t.Error("scenario produced neither sales nor payments — the fixtures or the parser drifted")
			}

			// Every receivable must point at a sale the same import carries;
			// otherwise the forecast shows money with no traceable origin.
			sales := make(map[payments.SaleID]bool, len(result.Sales))
			for _, s := range result.Sales {
				sales[s.ID] = true
			}
			for _, rc := range result.Receivables {
				if !sales[rc.SaleID] {
					t.Errorf("receivable references unknown sale %q", rc.SaleID)
				}
			}
		})
	}
}

func readScenario(t *testing.T, dir string) []Extract {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			names = append(names, e.Name())
		}
	}
	sortStrings(names)

	var extracts []Extract
	for _, name := range names {
		kind, ok := ClassifyFile(name)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		extracts = append(extracts, Extract{Kind: kind, Name: name, Data: data})
	}
	return extracts
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
