package main

import (
	"context"
	"log"
	"time"
	_ "time/tzdata" // embed zoneinfo so LoadLocation works on provided.al2

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/emerson/emerbot/apps/notifier/internal/notifier"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
	"github.com/emerson/emerbot/packages/whatsapp"
)

func main() {
	ctx := context.Background()

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

	// "Vence hoje" must use the pharmacy's local calendar day, not UTC.
	loc, err := time.LoadLocation(shared.Getenv("NOTIFIER_TIMEZONE", "America/Sao_Paulo"))
	if err != nil {
		log.Printf("load timezone: %v — falling back to UTC", err)
		loc = time.UTC
	}

	gen := orchestrator.NewTextGenerator(orchestrator.Config{
		FinanceStore: finStore,
		GeminiAPIKey: shared.Getenv("GEMINI_API_KEY", ""),
	})
	n := notifier.New(finStore, sessions, wa, phoneNumberID, loc, gen)

	// EventBridge Scheduler invokes with an event we don't need to inspect —
	// the job is the same every time.
	lambda.Start(func(ctx context.Context) error {
		_, err := n.Run(ctx)
		return err
	})
}
