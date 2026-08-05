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
	return []pkgfinance.Tool{analysisTool(store, loc)}
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
