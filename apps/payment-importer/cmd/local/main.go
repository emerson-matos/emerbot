package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/emerson/emerbot/apps/payment-importer/internal/app"
	"github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/shared"
)

// Local entrypoint: runs the exact same import pipeline as the Lambda, but reads
// a combined envelope from a file instead of S3 — so the flow is exercisable
// without S3/Lambda events. Writes to dynamodb-local when DYNAMODB_ENDPOINT is
// set, otherwise to an in-memory repo (parse-only smoke test).
func main() {
	ctx := context.Background()
	shared.InitSlog()

	file := flag.String("file", "", "path to a combined import envelope JSON")
	flag.Parse()
	if *file == "" {
		log.Fatal("-file is required (path to a combined envelope JSON)")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read %s: %v", *file, err)
	}

	var repo payments.Repository
	if endpoint := shared.Getenv("DYNAMODB_ENDPOINT", ""); endpoint != "" {
		table := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries")
		r, err := payments.NewDynamoDBRepository(ctx, table, endpoint, shared.FinanceLedgerID)
		if err != nil {
			log.Fatalf("payments repo: %v", err)
		}
		repo = r
	} else {
		log.Println("DYNAMODB_ENDPOINT not set — using in-memory repo (no persistence)")
		repo = payments.NewInMemoryRepository()
	}

	if err := app.New(repo).ProcessRaw(ctx, raw); err != nil {
		log.Fatalf("import %s: %v", *file, err)
	}
	log.Printf("imported %s", *file)
}
