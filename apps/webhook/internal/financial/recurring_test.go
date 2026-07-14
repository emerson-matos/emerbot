package financial

import (
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func TestParseRecorrenteParsesPagarMonthlySeries(t *testing.T) {
	t.Parallel()

	req, err := parseRecorrente("/recorrente pagar 350 aluguel mensal 12 Aluguel anual")
	if err != nil {
		t.Fatalf("parseRecorrente returned error: %v", err)
	}
	if req.Type != domain.EntryTypeExpense {
		t.Fatalf("expected expense type, got %+v", req)
	}
	if req.Amount != 35000 {
		t.Fatalf("expected amount 35000, got %d", req.Amount)
	}
	if req.Category != "aluguel" || req.Period != "mensal" || req.Occurrences != 12 {
		t.Fatalf("unexpected parsed request: %+v", req)
	}
	if req.Description != "Aluguel anual" {
		t.Fatalf("expected remaining text as description, got %q", req.Description)
	}
}

func TestParseRecorrenteParsesReceberWithStartDate(t *testing.T) {
	t.Parallel()

	req, err := parseRecorrente("/recorrente receber 800 convenio semanal 6 20/07 Repasse")
	if err != nil {
		t.Fatalf("parseRecorrente returned error: %v", err)
	}
	if req.Type != domain.EntryTypeIncome {
		t.Fatalf("expected income type, got %+v", req)
	}
	if req.StartDate.Day() != 20 || req.StartDate.Month() != 7 {
		t.Fatalf("expected start date July 20, got %+v", req.StartDate)
	}
	if req.Description != "Repasse" {
		t.Fatalf("expected remaining text as description, got %q", req.Description)
	}
}

func TestParseRecorrenteRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := []string{
		"/recorrente pagar 350 aluguel mensal 0",     // zero occurrences
		"/recorrente pagar 350 aluguel quinzenal -1", // negative occurrences (won't match regex)
		"/recorrente pagar aluguel mensal 12",        // missing amount
		"/recorrente pagar 350 aluguel bimestral 12", // unsupported period
	}
	for _, text := range cases {
		if _, err := parseRecorrente(text); err == nil {
			t.Errorf("expected error for %q", text)
		}
	}
}

func TestAddPeriodAdvancesByUnit(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		period string
		i      int
		want   time.Time
	}{
		{"diario", 3, time.Date(2026, 1, 18, 0, 0, 0, 0, time.UTC)},
		{"semanal", 2, time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)},
		{"quinzenal", 1, time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC)},
		{"mensal", 1, time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)},
		{"anual", 1, time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got := addPeriod(start, c.period, c.i)
		if !got.Equal(c.want) {
			t.Errorf("addPeriod(%v, %q, %d) = %v, want %v", start, c.period, c.i, got, c.want)
		}
	}
}
