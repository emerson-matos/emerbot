// Package agenttools assembles the tool set every LLM agent exposes, so the
// Gemini and Ollama agents cannot drift into offering different capabilities.
package agenttools

import (
	"time"

	"github.com/emerson/emerbot/packages/fiado"
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
	"github.com/emerson/emerbot/packages/shared"
)

// All returns the finance tools, the analysis tool and — when there is a
// caderninho to read — the fiado tools. The analysis tool is added here rather
// than inside finance.FinanceTools because the analytics package is built on
// top of packages/finance, and having finance reach back into it would be an
// import cycle.
//
// fiadoStore may be nil: an entrypoint that has not built one simply offers no
// caderninho tools, rather than offering tools that fail on the first call.
func All(store finance.Store, fiadoStore fiado.Store, dashboardURL string) []finance.Tool {
	loc := shared.PharmacyLocation()
	tools := finance.FinanceTools(store, dashboardURL, loc)
	tools = append(tools, analytics.Tools(store, loc)...)
	if fiadoStore != nil {
		tools = append(tools, fiadoTools(fiadoStore, loc)...)
	}
	return tools
}

// fiadoTools adapts the caderninho's tools to the shape the agents consume.
//
// The conversion exists so packages/fiado does not import packages/finance: the
// caderninho is a system apart (ADR-027), and a type import would be the first
// thread pulling the two back together. This package already knows both, which
// makes it the right place for the seam — and the two structs are field for
// field the same, so the adapter is the whole cost.
func fiadoTools(store fiado.Store, loc *time.Location) []finance.Tool {
	own := fiado.Tools(store, loc)
	out := make([]finance.Tool, 0, len(own))
	for _, t := range own {
		out = append(out, finance.Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
			Handler:     finance.ToolFunc(t.Handler),
		})
	}
	return out
}
