package waturn

import (
	"context"
	"log"

	"github.com/emerson/emerbot/packages/conversation"
	pkgfiado "github.com/emerson/emerbot/packages/fiado"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
	"github.com/emerson/emerbot/packages/whatsapp"
)

// NewFromEnv wires the worker from the environment. This is the wiring the
// webhook used to carry: the finance and fiado stores, the conversation history
// and the model all moved here with the turn itself, and the webhook no longer
// knows any of them exists (ADR-028).
//
// Missing configuration degrades the same way it always did — no finance table
// means the static responder, no sessions table means no dedup — because the
// same constructor runs `make run-webhook`-style local setups. What must not be
// missing in production is checked at the Lambda entrypoint, where it can fail
// loudly instead of quietly answering with a stub.
func NewFromEnv(ctx context.Context) *Worker {
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")

	cfg := orchestrator.Config{}
	if finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", ""); finTable != "" {
		store, err := pkgfinance.NewDynamoDBStore(ctx, finTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: finance store: %v", err)
		}
		cfg.FinanceStore = store
		// The caderninho lives in the same table, as a neighbour of the entries
		// in the user's partition — no resource of its own (ADR-027 §4).
		fiadoStore, err := pkgfiado.NewDynamoDBStore(ctx, finTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: fiado store: %v", err)
		}
		cfg.FiadoStore = fiadoStore
		cfg.GeminiAPIKey = shared.Getenv("GEMINI_API_KEY", "")
		// LLM_PROVIDER=ollama runs a local open-source model for dev (ADR-012);
		// unset keeps the Gemini/static path used in production.
		cfg.LLMProvider = shared.Getenv("LLM_PROVIDER", "")
		cfg.OllamaHost = shared.Getenv("OLLAMA_HOST", "")
		cfg.OllamaModel = shared.Getenv("OLLAMA_MODEL", "")
		cfg.DashboardURL = shared.Getenv("DASHBOARD_URL", "")
	}

	// Short-term chat history lives in its own TTL-managed table so the bot keeps
	// context across messages and cold starts. When unset, NewService falls back
	// to an in-memory store (fine locally, lost on every Lambda recycle).
	if convTable := shared.Getenv("CONVERSATIONS_TABLE", ""); convTable != "" {
		convStore, err := conversation.NewDynamoDBStore(ctx, convTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: conversation store: %v", err)
		}
		cfg.ShortTerm = convStore
	}

	var processed ProcessedStore
	if sessTable := shared.Getenv("WHATSAPP_SESSIONS_TABLE", ""); sessTable != "" {
		sessStore, err := wasession.NewDynamoDBStore(ctx, sessTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: session store: %v", err)
		}
		processed = sessStore
	} else {
		// Per-process only: it dedups within one run, which is all a local run
		// has, and nothing at all across Lambda invocations.
		processed = wasession.NewInMemoryStore()
	}

	return New(
		orchestrator.NewService(cfg),
		whatsapp.NewClientFromEnv(shared.Getenv("META_GRAPH_API_TOKEN", "")),
		processed,
		shared.GetenvInt("WHATSAPP_INBOUND_MAX_RECEIVES", DefaultMaxReceives),
	)
}
