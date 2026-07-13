package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/webhook/internal/app"
)

func main() {
	ctx := context.Background()

	secret, err := loadWebhookSecret(ctx)
	if err != nil {
		log.Fatal(err)
	}

	metaToken, err := loadMetaToken(ctx)
	if err != nil {
		log.Printf("warn: no meta token: %v", err)
	}

	application := app.NewFromEnv(secret, metaToken)
	lambda.Start(application.HandleLambda)
}
