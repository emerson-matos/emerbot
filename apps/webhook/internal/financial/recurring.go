package financial

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/whatsapp"
)

// recurrencePattern matches:
//
//	/recorrente <pagar|receber> <valor> <categoria> <periodo> <n> [resto]
var recurrencePattern = regexp.MustCompile(
	`(?i)^/recorrente\s+(pagar|receber)\s+(\d+(?:[,.]\d{1,2})?)\s+(\S+)\s+(diario|semanal|quinzenal|mensal|anual)\s+(\d+)\s*(.*)$`,
)

// recurrenceDatePattern matches an optional dd/mm[/yy[yy]] start date
// anywhere in the trailing text, same shape as parser.go's datePattern.
var recurrenceDatePattern = regexp.MustCompile(`(\d{1,2})/(\d{1,2})(?:/(\d{2,4}))?`)

// recurrenceRequest is a parsed /recorrente command, describing a whole
// series of pending entries rather than a single one.
type recurrenceRequest struct {
	Type        domain.EntryType
	Amount      int64 // centavos, per occurrence
	Category    string
	Period      string // "diario" | "semanal" | "quinzenal" | "mensal" | "anual"
	Occurrences int
	StartDate   time.Time
	Description string
	// Origin is set for income series: /recorrente receber schedules future
	// sales (a crediário instalment plan), so each occurrence is faturamento on
	// its own transaction date. Empty on expense series.
	Origin domain.IncomeOrigin
}

func parseRecorrente(text string) (recurrenceRequest, error) {
	m := recurrencePattern.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return recurrenceRequest{}, fmt.Errorf("formato inválido, use: /recorrente <pagar|receber> <valor> <categoria> <periodo> <n> [data] [descrição]")
	}

	verb := strings.ToLower(m[1])
	amount, err := whatsapp.ParseAmount(strings.ReplaceAll(m[2], ",", "."))
	if err != nil || amount <= 0 {
		return recurrenceRequest{}, fmt.Errorf("valor inválido: %q", m[2])
	}
	category := strings.ToLower(m[3])
	period := strings.ToLower(m[4])

	n, err := strconv.Atoi(m[5])
	if err != nil || n <= 0 {
		return recurrenceRequest{}, fmt.Errorf("número de ocorrências inválido: %q", m[5])
	}

	entryType := domain.EntryTypeExpense
	origin := domain.IncomeOrigin("")
	if verb == "receber" {
		entryType = domain.EntryTypeIncome
		origin = domain.OriginVenda
	}

	rest := strings.TrimSpace(m[6])
	start := time.Now().UTC()
	desc := rest
	if dm := recurrenceDatePattern.FindStringSubmatch(rest); dm != nil {
		day, _ := strconv.Atoi(dm[1])
		month, _ := strconv.Atoi(dm[2])
		year := start.Year()
		if dm[3] != "" {
			if y, err := strconv.Atoi(dm[3]); err == nil {
				if y < 100 {
					y += 2000
				}
				year = y
			}
		}
		start = time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		desc = strings.TrimSpace(strings.Replace(rest, dm[0], "", 1))
	}

	return recurrenceRequest{
		Type:        entryType,
		Amount:      amount,
		Category:    category,
		Period:      period,
		Occurrences: n,
		StartDate:   start,
		Description: desc,
		Origin:      origin,
	}, nil
}

// addPeriod returns the due date for the ith (0-based) occurrence of a
// recurrence starting at start.
//
// The month-based periods go through domain.AddMonths so a series that starts
// on the 31st keeps one occurrence per month, clamped to each month's last day.
// time.AddDate would overflow instead — 31 January plus one month is 3 March —
// leaving February with no instalment and March with two.
func addPeriod(start time.Time, period string, i int) time.Time {
	switch period {
	case "diario":
		return start.AddDate(0, 0, i)
	case "semanal":
		return start.AddDate(0, 0, 7*i)
	case "quinzenal":
		return start.AddDate(0, 0, 15*i)
	case "anual":
		return domain.AddMonths(start, 12*i)
	default: // "mensal"
		return domain.AddMonths(start, i)
	}
}

func periodLabel(period string) string {
	switch period {
	case "diario":
		return "diário"
	case "semanal":
		return "semanal"
	case "quinzenal":
		return "quinzenal"
	case "anual":
		return "anual"
	default:
		return "mensal"
	}
}
