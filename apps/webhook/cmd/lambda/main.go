package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/webhook/internal/app"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	application := app.NewDefault(shared.Getenv("WEBHOOK_SECRET", "local-secret"))
	lambda.Start(application.HandleLambda)
}

