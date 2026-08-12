package main

import (
	"log"
	_ "time/tzdata" // embed zoneinfo so LoadLocation works on provided.al2

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/webhook/internal/app"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	shared.InitSlog()
	secret := shared.Getenv("WEBHOOK_SECRET", "")
	if secret == "" {
		log.Fatal("WEBHOOK_SECRET is required")
	}
	// The deployed webhook has exactly one thing to do with a message, and this
	// is where it puts it. Starting without a queue would mean answering Meta
	// 500 on every message instead of failing here, where the cause is legible.
	if shared.Getenv("WHATSAPP_INBOUND_QUEUE_URL", "") == "" {
		log.Fatal("WHATSAPP_INBOUND_QUEUE_URL is required")
	}
	metaToken := shared.Getenv("META_GRAPH_API_TOKEN", "")

	application := app.NewFromEnv(secret, metaToken)
	lambda.Start(application.HandleLambda)
}
