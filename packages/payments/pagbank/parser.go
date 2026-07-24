// Package pagbank translates PagBank's EDI extracts (transactional + financial)
// into the canonical payments model. It is a pure, deterministic translator:
// given the same envelope bytes it always yields the same ImportResult, and it
// touches no clock, DB, network, or business rules — it only maps fields.
//
// The field names and shapes here follow the *real* EDI API payload (lowercase
// snake_case, amounts as JSON numbers), not the legacy uppercase PDF layout —
// see PagBank's official test scenarios, mirrored under testdata/. Each extract
// is the raw API response object, `{ "detalhes": [ {…}, … ], "pagination": {…} }`.
//
// Envelope shape (assembled by the ingestion script from the two EDI API
// responses; see the plan):
//
//	{ "provider":"pagbank", "importedAt":"…", "date":"2026-07-23",
//	  "transactional":{ "detalhes":[ {…} ] }, "financial":{ "detalhes":[ {…} ] } }
package pagbank

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/payments"
)

// Parser implements payments.Parser for PagBank.
type Parser struct{}

// New returns a PagBank parser.
func New() Parser { return Parser{} }

// envelope is PagBank's own strongly-typed import envelope. json.RawMessage is
// appropriate here — inside a JSON struct — so each extract is decoded lazily.
type envelope struct {
	Provider      payments.Provider   `json:"provider"`
	ImportedAt    time.Time           `json:"importedAt"` // audit only; ignored by the parser
	Date          domain.CalendarDate `json:"date"`
	Transactional json.RawMessage     `json:"transactional"`
	Financial     json.RawMessage     `json:"financial"`
}

// tipoEventoVenda is the only event this POC imports (Venda ou Pagamento);
// cancellations/chargebacks/adjustments (events 3,5,6,26,27…) are out of scope.
const tipoEventoVenda = "1"

// transactionalRecord is one "detalhe" line of the transactional extract. In
// múltiplo there is one line per installment, all sharing codigo_transacao.
type transactionalRecord struct {
	TipoEvento            ediStr `json:"tipo_evento"`
	CodigoTransacao       ediStr `json:"codigo_transacao"`
	DataVendaAjuste       ediStr `json:"data_venda_ajuste"`
	ValorTotalTransacao   ediStr `json:"valor_total_transacao"`
	ValorLiquidoTransacao ediStr `json:"valor_liquido_transacao"`
	ValorParcela          ediStr `json:"valor_parcela"`
	Parcela               ediStr `json:"parcela"`
	QuantidadeParcelas    ediStr `json:"quantidade_parcelas"`
	DataPrevistaPagamento ediStr `json:"data_prevista_pagamento"`
	MeioPagamento         ediStr `json:"meio_pagamento"`
	InstituicaoFinanceira ediStr `json:"instituicao_financeira"`
}

// financialRecord is one "detalhe" line of the financial extract (a liquidation).
type financialRecord struct {
	TipoEvento       ediStr `json:"tipo_evento"`
	CodigoTransacao  ediStr `json:"codigo_transacao"`
	DataMovimentacao ediStr `json:"data_movimentacao"`
	ValorParcela     ediStr `json:"valor_parcela"`
}

// Parse decodes the envelope and maps both extracts into canonical items.
func (Parser) Parse(raw []byte) (payments.ImportResult, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return payments.ImportResult{}, fmt.Errorf("decode envelope: %w", err)
	}

	result := payments.ImportResult{Provider: env.Provider, SourceDate: env.Date}

	sales, receivables, err := parseTransactional(env.Transactional)
	if err != nil {
		return payments.ImportResult{}, err
	}
	result.Sales = sales
	result.Receivables = receivables

	result.Payments, err = parseFinancial(env.Financial)
	if err != nil {
		return payments.ImportResult{}, err
	}
	return result, nil
}

// decodeRecords unmarshals an extract into dst (a *[]record). It accepts the
// real API response object `{ "detalhes": [...] }` and, tolerantly, a bare array
// (a pre-extracted detalhes list).
func decodeRecords(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	var wrapper struct {
		Detalhes json.RawMessage `json:"detalhes"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Detalhes) > 0 {
		return json.Unmarshal(wrapper.Detalhes, dst)
	}
	return json.Unmarshal(raw, dst)
}

func parseTransactional(raw json.RawMessage) ([]payments.Sale, []payments.ExpectedReceivable, error) {
	var records []transactionalRecord
	if err := decodeRecords(raw, &records); err != nil {
		return nil, nil, fmt.Errorf("decode transactional: %w", err)
	}

	// One sale per codigo_transacao (deduped across its installment lines),
	// preserving first-seen order for a deterministic result.
	salesByID := make(map[string]*payments.Sale)
	var order []string
	var receivables []payments.ExpectedReceivable

	for i, rec := range records {
		if string(rec.TipoEvento) != tipoEventoVenda {
			continue
		}
		externalID := strings.TrimSpace(string(rec.CodigoTransacao))
		if externalID == "" {
			return nil, nil, fmt.Errorf("transactional record %d: empty codigo_transacao", i)
		}
		saleID := payments.NewSaleID(payments.ProviderPagBank, externalID)
		installments := parseInstallments(string(rec.QuantidadeParcelas))

		if _, ok := salesByID[externalID]; !ok {
			saleDate, err := parseEDIDate(string(rec.DataVendaAjuste))
			if err != nil {
				return nil, nil, fmt.Errorf("transactional record %d sale date: %w", i, err)
			}
			gross, err := payments.ParseCentavos(string(rec.ValorTotalTransacao))
			if err != nil {
				return nil, nil, fmt.Errorf("transactional record %d gross: %w", i, err)
			}
			net, err := payments.ParseCentavos(string(rec.ValorLiquidoTransacao))
			if err != nil {
				return nil, nil, fmt.Errorf("transactional record %d net: %w", i, err)
			}
			salesByID[externalID] = &payments.Sale{
				ID: saleID, Provider: payments.ProviderPagBank, ExternalID: externalID,
				SaleDate: saleDate, GrossAmount: gross, NetAmount: net, FeeAmount: gross - net,
				Method:       mapMeioPagamento(string(rec.MeioPagamento)),
				Brand:        strings.TrimSpace(string(rec.InstituicaoFinanceira)),
				Installments: installments,
			}
			order = append(order, externalID)
		}

		expectedDate, err := parseEDIDate(string(rec.DataPrevistaPagamento))
		if err != nil {
			return nil, nil, fmt.Errorf("transactional record %d expected date: %w", i, err)
		}
		amount, err := payments.ParseCentavos(string(rec.ValorParcela))
		if err != nil {
			return nil, nil, fmt.Errorf("transactional record %d parcela: %w", i, err)
		}
		receivables = append(receivables, payments.ExpectedReceivable{
			Provider: payments.ProviderPagBank, SaleID: saleID, ExpectedDate: expectedDate,
			Amount: amount, InstallmentNumber: parseParcela(string(rec.Parcela)), InstallmentTotal: installments,
		})
	}

	sales := make([]payments.Sale, 0, len(order))
	for _, id := range order {
		sales = append(sales, *salesByID[id])
	}
	return sales, receivables, nil
}

func parseFinancial(raw json.RawMessage) ([]payments.Payment, error) {
	var records []financialRecord
	if err := decodeRecords(raw, &records); err != nil {
		return nil, fmt.Errorf("decode financial: %w", err)
	}
	var pays []payments.Payment
	for i, rec := range records {
		if string(rec.TipoEvento) != tipoEventoVenda {
			continue
		}
		externalID := strings.TrimSpace(string(rec.CodigoTransacao))
		if externalID == "" {
			return nil, fmt.Errorf("financial record %d: empty codigo_transacao", i)
		}
		payDate, err := parseEDIDate(string(rec.DataMovimentacao))
		if err != nil {
			return nil, fmt.Errorf("financial record %d payment date: %w", i, err)
		}
		amount, err := payments.ParseCentavos(string(rec.ValorParcela))
		if err != nil {
			return nil, fmt.Errorf("financial record %d amount: %w", i, err)
		}
		pays = append(pays, payments.Payment{
			Provider: payments.ProviderPagBank, SaleID: payments.NewSaleID(payments.ProviderPagBank, externalID),
			PaymentDate: payDate, Amount: amount,
		})
	}
	return pays, nil
}

// mapMeioPagamento maps the EDI "meio_pagamento" code (support table, PDF p.31)
// to a canonical PaymentMethod. The real API omits leading zeros ("3", "8"),
// while the legacy layout zero-pads ("03", "08"); normalizeCode accepts both.
func mapMeioPagamento(code string) payments.PaymentMethod {
	switch normalizeCode(code) {
	case "3", "14": // Cartão de Crédito, Crédito Pré-Pago
		return payments.MethodCredito
	case "1", "8", "15": // Débito Online, Cartão de Débito, Débito Pré-Pago
		return payments.MethodDebito
	case "11": // Pix
		return payments.MethodPix
	case "2": // Boleto
		return payments.MethodBoleto
	default:
		return payments.MethodOutros
	}
}

// normalizeCode trims whitespace and leading zeros so "03" and "3" compare equal.
func normalizeCode(s string) string {
	s = strings.TrimLeft(strings.TrimSpace(s), "0")
	if s == "" {
		return "0"
	}
	return s
}

// parseInstallments maps quantidade_parcelas ("0" débito à vista, "1" crédito à
// vista, "2"–"18" parcelado) to a count, floored at 1.
func parseInstallments(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func parseParcela(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// parseEDIDate accepts both "2006-01-02" and the raw "AAAAMMDD" form the spec
// describes, so the parser is tolerant of either serialization.
func parseEDIDate(s string) (domain.CalendarDate, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return domain.CalendarDate{}, fmt.Errorf("empty date")
	}
	if d, err := domain.ParseCalendarDate(s); err == nil {
		return d, nil
	}
	if t, err := time.Parse("20060102", s); err == nil {
		return domain.NewCalendarDate(t), nil
	}
	return domain.CalendarDate{}, fmt.Errorf("unrecognized date %q", s)
}

// ediStr decodes an EDI field that may arrive as a JSON string or a bare number
// (amounts come as numbers, codes as strings).
type ediStr string

func (e *ediStr) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*e = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*e = ediStr(s)
		return nil
	}
	*e = ediStr(b) // bare number literal
	return nil
}
