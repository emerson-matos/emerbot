// Package dynamostore holds the DynamoDB plumbing every store in this repo
// shares: the narrow client interface the stores actually depend on, and the
// single place that builds a real client from AWS config.
//
// Stores depend on API rather than on *dynamodb.Client so they can be
// exercised against a fake (see dynamotest) instead of a live table. The
// interface is deliberately narrow — it lists only the operations this repo
// issues, so a fake has a small, fully-implementable surface.
package dynamostore

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// API is the subset of *dynamodb.Client used across this repo.
type API interface {
	PutItem(ctx context.Context, in *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, in *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	// UpdateItem mutates one item in place. It is here for the counters a Put
	// cannot express: read-modify-write loses concurrent increments, while
	// "ADD Saldo :n" applies the delta inside DynamoDB (see packages/fiado).
	UpdateItem(ctx context.Context, in *dynamodb.UpdateItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	DeleteItem(ctx context.Context, in *dynamodb.DeleteItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Query(ctx context.Context, in *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	Scan(ctx context.Context, in *dynamodb.ScanInput, opts ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	BatchWriteItem(ctx context.Context, in *dynamodb.BatchWriteItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
	TransactWriteItems(ctx context.Context, in *dynamodb.TransactWriteItemsInput, opts ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

// The real client must satisfy API, and API must remain usable with the SDK's
// paginators — stores read through them.
var (
	_ API                     = (*dynamodb.Client)(nil)
	_ dynamodb.QueryAPIClient = API(nil)
	_ dynamodb.ScanAPIClient  = API(nil)
)

// NewClient builds a DynamoDB client from the ambient AWS config. If endpoint
// is non-empty it overrides the resolved endpoint, which is how every store
// points at DynamoDB Local under podman compose.
func NewClient(ctx context.Context, endpoint string) (*dynamodb.Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if endpoint != "" {
		opts = append(opts, awsconfig.WithBaseEndpoint(endpoint))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}
