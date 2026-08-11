package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"

	"google.golang.org/genai"

	pkgfinance "github.com/emerson/emerbot/packages/finance"
)

// Tools returns the analysis tools exposed to the AI agents. It lives here
// rather than alongside the other finance tools because this package sits on
// top of packages/finance — callers combine the two:
//
//	tools := append(finance.FinanceTools(store, url), analytics.Tools(store, loc)...)
//
// loc is the timezone whose calendar day defines "today" (nil means UTC).
func Tools(store LedgerReader, loc *time.Location) []pkgfinance.Tool {
	return []pkgfinance.Tool{analysisTool(store, loc), dayTargetTool(store, loc)}
}

// analysisTool answers the open-ended questions the per-month summary cannot:
// "como estamos?", "vai bater a meta?", "o que devo fazer?".
func analysisTool(store LedgerReader, loc *time.Location) pkgfinance.Tool {
	if loc == nil {
		loc = time.UTC
	}

	return pkgfinance.Tool{
		Name: "get_analysis",
		Description: "Retorna a análise financeira completa de um mês: saúde financeira, " +
			"tendências vs mês passado, comparação da semana atual com a anterior, " +
			"progresso, meta de faturamento de hoje e de amanhã, projeção de caixa " +
			"do mês e do dia seguinte e " +
			"recomendações. Use para perguntas abertas como \"como estamos?\", " +
			"\"vamos bater a meta?\", \"como estamos para amanhã?\" ou \"o que devo fazer?\". A resposta lista em " +
			"secoes_disponiveis os detalhamentos que existem mas não vêm por " +
			"padrão; para trazê-los, chame de novo pedindo em \"secoes\".",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"month": {Type: genai.TypeString, Description: "Mês no formato YYYY-MM (padrão: mês atual)"},
				// Named in the schema rather than left free-form: an enum is what
				// stops the model from inventing a section and reading the silence
				// that follows as "a farmácia não tem esses dados".
				"secoes": {
					Type:        genai.TypeArray,
					Items:       &genai.Schema{Type: genai.TypeString, Enum: sectionNames()},
					Description: sectionParamDescription(),
				},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Month  string   `json:"month"`
				Secoes []string `json:"secoes"`
			}
			// An empty argument object is a valid call ("como estamos?" needs
			// no month), so only malformed JSON is an error.
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, fmt.Errorf("parse args: %w", err)
				}
			}

			now := time.Now().In(loc)
			if args.Month == "" {
				args.Month = domain.MonthOf(now)
			}

			analysis, err := Assemble(ctx, store, userID, args.Month, now)
			if err != nil {
				return nil, fmt.Errorf("analysis for %s: %w", args.Month, err)
			}
			return analysis.ToolPayload(parseSections(args.Secoes)...), nil
		},
	}
}

// parseSections keeps only the names this package actually serves. An unknown
// one is dropped rather than erroring: the request is still answerable — the
// base analysis is right there — and failing the whole call over a misspelt
// section would turn "como estamos?" into an error the user never asked for.
func parseSections(names []string) []Section {
	out := make([]Section, 0, len(names))
	for _, n := range names {
		for _, s := range sectionCatalog {
			if string(s.Name) == n {
				out = append(out, s.Name)
				break
			}
		}
	}
	return out
}

// sectionNames is the enum the tool schema advertises, off the same list
// AllSections walks — the schema and the catalog cannot name different sets.
func sectionNames() []string {
	sections := AllSections()
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		out = append(out, string(s))
	}
	return out
}

// sectionParamDescription spells out what each section brings, so the model can
// pick from the schema alone on its first call — before it has seen a response
// carrying secoes_disponiveis.
func sectionParamDescription() string {
	parts := make([]string, 0, len(sectionCatalog))
	for _, s := range sectionCatalog {
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Name, s.What))
	}
	return "Detalhamentos extras a incluir na resposta. Opções: " + strings.Join(parts, "; ") + "."
}

// dayTargetToolName is the tool that prices a named day. It is referenced from
// the analysis payload as well, so the model is told where to go rather than
// left to conclude that today is the only day this analysis can speak about.
const dayTargetToolName = "get_meta_do_dia"

// maxDayTargets caps how many dates one call prices. Each distinct month behind
// them costs a full Assemble, and a model asked "e o resto da semana?" would
// otherwise happily send thirty. Beyond the cap the list is cut and says it was
// cut (ADR-015) — a week is more than anyone acts on at once anyway.
const maxDayTargets = 7

// dayTargetTool answers "quanto preciso vender no dia X?" for any X.
//
// It exists because the answer used to be a field, and a field is per-day: the
// analysis shipped meta_de_hoje, then meta_de_hoje and meta_de_amanha, and the
// question after "e amanhã?" is "e no sábado?". The day is a parameter here,
// and what comes back says which regime it is in — a day that closed reports
// what it did, a day still ahead reports what is being asked of it. See
// ADR-021.
func dayTargetTool(store LedgerReader, loc *time.Location) pkgfinance.Tool {
	if loc == nil {
		loc = time.UTC
	}

	return pkgfinance.Tool{
		Name: dayTargetToolName,
		Description: "Meta de faturamento de um ou mais dias específicos, já escalada pelo " +
			"ritmo daquele dia da semana. Use para \"quanto preciso vender amanhã?\", " +
			"\"e no sábado?\", \"como foi o dia 12?\". Cada dia volta com " +
			"apuracao: \"realizado\" (dia fechado: traz o que vendeu, sem meta), " +
			"\"em_curso\" (hoje: meta e o que já vendeu) ou \"projetado\" (dia " +
			"futuro: meta, supondo que os dias antes dele fechem nas metas deles). " +
			"A meta nunca é menor que a média daquele dia da semana; quando ela é " +
			"igual à média, origem_da_meta diz por quê. " +
			"NUNCA responda uma pergunta sobre um dia com a média do dia da semana: " +
			"a média é o que aquele dia costuma faturar, a meta é o que ele precisa faturar.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"datas": {
					Type:  genai.TypeArray,
					Items: &genai.Schema{Type: genai.TypeString},
					Description: fmt.Sprintf(
						"Datas no formato YYYY-MM-DD, no máximo %d. Padrão: hoje.", maxDayTargets,
					),
				},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Datas []string `json:"datas"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return nil, fmt.Errorf("parse args: %w", err)
				}
			}

			now := time.Now().In(loc)
			dates, err := parseDayTargetDates(args.Datas, now)
			if err != nil {
				return nil, err
			}

			payload := map[string]any{}
			if len(dates) > maxDayTargets {
				// Said out loud rather than silently dropped: a model that asked
				// for ten days and got seven would present the seven as the whole
				// answer.
				payload["truncated"] = true
				payload["warning"] = fmt.Sprintf(
					"Mostrando %d de %d datas. Peça o resto em outra chamada.", maxDayTargets, len(dates),
				)
				dates = dates[:maxDayTargets]
			}

			days, err := dayTargets(ctx, store, userID, dates, now)
			if err != nil {
				return nil, err
			}
			payload["dias"] = days
			return payload, nil
		},
	}
}

// parseDayTargetDates turns the argument into calendar dates, keeping the
// caller's order and dropping repeats — a model that asks for "amanhã e
// 06/08" on the 5th means one day, and pricing it twice would read as two.
//
// An empty list means today. A malformed date is an error rather than a
// silently skipped entry: on a financial answer, "não achei nada para essa
// data" and "você digitou a data errada" must not render the same.
func parseDayTargetDates(raw []string, now time.Time) ([]domain.CalendarDate, error) {
	if len(raw) == 0 {
		return []domain.CalendarDate{domain.NewCalendarDate(now)}, nil
	}
	seen := make(map[string]bool, len(raw))
	dates := make([]domain.CalendarDate, 0, len(raw))
	for _, s := range raw {
		d, err := domain.ParseCalendarDate(s)
		if err != nil {
			return nil, fmt.Errorf("data %q: use o formato YYYY-MM-DD", s)
		}
		if seen[d.String()] {
			continue
		}
		seen[d.String()] = true
		dates = append(dates, d)
	}
	return dates, nil
}

// dayTargets prices every date, assembling each month behind them exactly once.
// Grouping is the whole reason this takes a list: "sexta, sábado e domingo" is
// three days of one month and has no business costing three analyses.
//
// The order of the answer follows the order asked, not the order assembled.
func dayTargets(ctx context.Context, store LedgerReader, userID string, dates []domain.CalendarDate, now time.Time) ([]map[string]any, error) {
	currentMonth := now.Format(domain.MonthLayout)
	out := make([]map[string]any, len(dates))

	byMonth := map[string][]int{}
	for i, d := range dates {
		month := d.Format(domain.MonthLayout)
		// A month that has not started has no gap to distribute and no history of
		// its own, and monthClock cannot tell it from one that has ended — it
		// would answer "mes_fechado" for September asked about in August. Named
		// here, before anything is fetched for a month there is nothing to fetch.
		if month > currentMonth {
			out[i] = dayTargetToolPayload(DayTarget{
				State: DayTargetFutureMonth,
				Basis: DayProjected,
				Date:  d.String(),
				Day:   d.Time().Weekday(),
			})
			continue
		}
		byMonth[month] = append(byMonth[month], i)
	}

	for month, indexes := range byMonth {
		analysis, err := Assemble(ctx, store, userID, month, now)
		if err != nil {
			return nil, fmt.Errorf("analysis for %s: %w", month, err)
		}
		// One read for the month's sales, on the transaction basis, because
		// faturamento is dated by the day of the sale. The Analysis carries the
		// month's totals and not its days, and widening it with a daily series
		// would put thirty-one more numbers into every stored snapshot for the
		// sake of the one day someone asked about.
		revenue, err := revenueByDay(ctx, store, userID, month)
		if err != nil {
			return nil, err
		}
		for _, i := range indexes {
			date := dates[i]
			out[i] = dayTargetToolPayload(analysis.DayTargetOn(date, revenue[date.String()], now))
		}
	}
	return out, nil
}

// revenueByDay totals a month's faturamento per calendar day, keyed
// "YYYY-MM-DD". Origin decides what counts, not category — see ADR-016.
func revenueByDay(ctx context.Context, store LedgerReader, userID, month string) (map[string]int64, error) {
	entries, err := monthEntries(ctx, store, userID, month, pkgfinance.BasisTransaction)
	if err != nil {
		return nil, err
	}
	byDay := make(map[string]int64, len(entries))
	for _, e := range entries {
		if domain.IsRevenue(e) {
			byDay[e.TransactionDate.String()] += e.Amount
		}
	}
	return byDay, nil
}
