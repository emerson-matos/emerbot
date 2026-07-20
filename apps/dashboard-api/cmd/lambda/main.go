package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/app"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	ctx := context.Background()

	finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "")

	finStore, err := pkgfinance.NewDynamoDBStore(ctx, finTable, "")
	if err != nil {
		log.Fatalf("finance store: %v", err)
	}

	application := app.NewGateway(finStore)
	lambda.Start(application.HandleLambda)
}
