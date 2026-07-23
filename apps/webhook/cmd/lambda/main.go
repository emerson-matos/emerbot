package main

import (
	"log"

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
	metaToken := shared.Getenv("META_GRAPH_API_TOKEN", "")

	application := app.NewFromEnv(secret, metaToken)
	lambda.Start(application.HandleLambda)
}
