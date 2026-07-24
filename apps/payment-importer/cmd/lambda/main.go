package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/url"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/emerson/emerbot/apps/payment-importer/internal/app"
	"github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/shared"
)

// The importer is triggered by an S3 ObjectCreated event: the ingestion script
// uploads one combined envelope, and this Lambda reads it, parses it and
// persists the canonical data. It only orchestrates — no business logic here.
func main() {
	ctx := context.Background()
	shared.InitSlog()

	table := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "")
	if table == "" {
		log.Fatal("FINANCIAL_ENTRIES_TABLE is required")
	}
	repo, err := payments.NewDynamoDBRepository(ctx, table, "", shared.FinanceLedgerID)
	if err != nil {
		log.Fatalf("payments repo: %v", err)
	}
	application := app.New(repo)

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}
	s3c := s3.NewFromConfig(cfg)

	lambda.Start(func(ctx context.Context, event events.S3Event) error {
		for _, rec := range event.Records {
			bucket := rec.S3.Bucket.Name
			// S3 event object keys are URL-encoded (spaces as '+', etc.).
			key, err := url.QueryUnescape(rec.S3.Object.Key)
			if err != nil {
				return fmt.Errorf("unescape key %q: %w", rec.S3.Object.Key, err)
			}
			out, err := s3c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
			if err != nil {
				return fmt.Errorf("get s3://%s/%s: %w", bucket, key, err)
			}
			raw, err := io.ReadAll(out.Body)
			_ = out.Body.Close()
			if err != nil {
				return fmt.Errorf("read s3://%s/%s: %w", bucket, key, err)
			}
			if err := application.ProcessRaw(ctx, raw); err != nil {
				return fmt.Errorf("import %s: %w", key, err)
			}
			slog.Info("imported payment envelope", "bucket", bucket, "key", key)
		}
		return nil
	})
}
