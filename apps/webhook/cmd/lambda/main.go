package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/webhook/internal/app"
)

func main() {
	secret, err := loadWebhookSecret(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	application := app.NewDefault(secret)
	lambda.Start(application.HandleLambda)
}
