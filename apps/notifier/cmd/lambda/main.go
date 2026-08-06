package main

import (
	"context"
	"log"
	_ "time/tzdata" // embed zoneinfo so LoadLocation works on provided.al2

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/emerson/emerbot/apps/notifier/internal/notifier"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
	"github.com/emerson/emerbot/packages/whatsapp"
)

// scheduledRun is the payload EventBridge Scheduler is configured to send. Any
// other field in the event is ignored, so a schedule with no input at all
// unmarshals to the zero value — which ParseRunKind reads as the daily digest.
type scheduledRun struct {
	Run string `json:"run"`
}

func main() {
	ctx := context.Background()
	// Without this the notifier's slog output goes through the default handler,
	// which ignores LOG_LEVEL — so LOG_LEVEL=debug on the Lambda would silently
	// do nothing when someone is trying to work out why a digest never arrived.
	shared.InitSlog()

	finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "")
	finStore, err := pkgfinance.NewDynamoDBStore(ctx, finTable, "")
	if err != nil {
		log.Fatalf("finance store: %v", err)
	}

	sessTable := shared.Getenv("WHATSAPP_SESSIONS_TABLE", "")
	if sessTable == "" {
		log.Fatal("WHATSAPP_SESSIONS_TABLE is required")
	}
	sessions, err := wasession.NewDynamoDBStore(ctx, sessTable, "")
	if err != nil {
		log.Fatalf("session store: %v", err)
	}

	metaToken := shared.Getenv("META_GRAPH_API_TOKEN", "")
	wa := whatsapp.NewClientFromEnv(metaToken)
	if wa == nil {
		log.Fatal("no WhatsApp client configured (set META_GRAPH_API_TOKEN or REPLY_URL)")
	}

	phoneNumberID := shared.Getenv("WHATSAPP_PHONE_NUMBER_ID", "")
	if phoneNumberID == "" {
		log.Fatal("WHATSAPP_PHONE_NUMBER_ID is required")
	}

	// "Vence hoje" must use the pharmacy's local calendar day, not UTC — and
	// the same one the dashboard and the bot use, so the digest never talks
	// about a different day than the analysis it now carries.
	loc := shared.PharmacyLocation()

	// The digest rewrites a static draft into friendlier prose — a one-shot text
	// call with a system prompt, no tools and no history — so it uses the
	// direct writer, not the tool-calling agent generator (which requires a
	// conversation history the digest never has; see NewDigestGenerator).
	gen := orchestrator.NewDigestGenerator(orchestrator.Config{
		GeminiAPIKey: shared.Getenv("GEMINI_API_KEY", ""),
	})
	dashboardURL := shared.Getenv("DASHBOARD_URL", "")
	n := notifier.New(finStore, sessions, wa, phoneNumberID, dashboardURL, loc, gen)

	// Two schedules invoke this Lambda: the morning digest and the afternoon
	// reminder of what is still due today. Which one is in the event, not in the
	// clock — an EventBridge flexible window and a retry both move the hour a run
	// actually lands on, and reading the job off time.Now() would eventually send
	// the wrong message. An event without the field (any schedule that predates
	// it) is the digest.
	lambda.Start(func(ctx context.Context, event scheduledRun) error {
		kind, err := notifier.ParseRunKind(event.Run)
		if err != nil {
			return err
		}
		_, err = n.Run(ctx, kind)
		return err
	})
}
