package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/app"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	pkgpayments "github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	ctx := context.Background()

	finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "")

	finStore, err := pkgfinance.NewDynamoDBStore(ctx, finTable, "")
	if err != nil {
		log.Fatalf("finance store: %v", err)
	}

	// Imported payment data shares the finance table, partitioned under the
	// shared pharmacy ledger.
	payRepo, err := pkgpayments.NewDynamoDBRepository(ctx, finTable, "", shared.FinanceLedgerID)
	if err != nil {
		log.Fatalf("payments repo: %v", err)
	}

	application := app.NewGateway(finStore, payRepo)
	lambda.Start(application.HandleLambda)
}
