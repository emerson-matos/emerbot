package main

import (
	"context"
	"log"
	_ "time/tzdata" // embed zoneinfo so LoadLocation works on provided.al2

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/app"
	pkgfiado "github.com/emerson/emerbot/packages/fiado"
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

	// The caderninho lives in the same table, as neighbours of the entries in
	// the user's partition — no resource of its own (ADR-027 §4).
	fiadoStore, err := pkgfiado.NewDynamoDBStore(ctx, finTable, "")
	if err != nil {
		log.Fatalf("fiado store: %v", err)
	}

	application := app.NewGateway(finStore, payRepo, fiadoStore)
	lambda.Start(application.HandleLambda)
}
