package analytics

import (
	"context"
	"encoding/json"
	"fmt"
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
			"progresso e ritmo necessário para bater a meta, projeção de caixa e " +
			"recomendações. Use para perguntas abertas como \"como estamos?\", " +
			"\"vamos bater a meta?\" ou \"o que devo fazer?\".",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"month": {Type: genai.TypeString, Description: "Mês no formato YYYY-MM (padrão: mês atual)"},
			},
		},
		Handler: func(ctx context.Context, userID string, raw json.RawMessage) (any, error) {
			var args struct {
				Month string `json:"month"`
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
			return analysis.ToolPayload(), nil
		},
	}
}
