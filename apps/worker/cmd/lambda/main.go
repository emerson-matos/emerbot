package main

import (
	"context"
	"log"
	_ "time/tzdata" // embed zoneinfo so LoadLocation works on provided.al2

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/waturn"
)

func main() {
	shared.InitSlog()

	// Both are optional in NewFromEnv, because a local run degrades rather than
	// refusing to start. In the Lambda they are not: without the sessions table
	// the dedup mark is per-process, which is no dedup at all across
	// invocations (ADR-029), and without the finance table the agent silently
	// becomes the static responder.
	if shared.Getenv("WHATSAPP_SESSIONS_TABLE", "") == "" {
		log.Fatal("WHATSAPP_SESSIONS_TABLE is required")
	}
	if shared.Getenv("FINANCIAL_ENTRIES_TABLE", "") == "" {
		log.Fatal("FINANCIAL_ENTRIES_TABLE is required")
	}

	w := waturn.NewFromEnv(context.Background())
	lambda.Start(w.HandleSQS)
}
