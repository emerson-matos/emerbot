// Package agenttools assembles the tool set every LLM agent exposes, so the
// Gemini and Ollama agents cannot drift into offering different capabilities.
package agenttools

import (
	"github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/finance/analytics"
	"github.com/emerson/emerbot/packages/shared"
)

// All returns the finance tools plus the analysis tool. The analysis tool is
// added here rather than inside finance.FinanceTools because the analytics
// package is built on top of packages/finance, and having finance reach back
// into it would be an import cycle.
func All(store finance.Store, dashboardURL string) []finance.Tool {
	loc := shared.PharmacyLocation()
	tools := finance.FinanceTools(store, dashboardURL, loc)
	return append(tools, analytics.Tools(store, loc)...)
}
