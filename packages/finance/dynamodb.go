package finance

import (
	"context"

	"github.com/emerson/emerbot/packages/dynamostore"
)

// The DynamoDB implementation of Store is split by concern across this file
// and dynamodb_entries.go, dynamodb_summaries.go, dynamodb_catalog.go and
// dynamodb_notifications.go. This file holds the key layout every one of them
// shares, plus the store type and its constructors.

const (
	pkPrefix    = "USER#"
	entryPrefix = "ENTRY#"
	catPrefix   = "CAT#"
	goalPrefix  = "GOAL#"

	// notifPrefsSK is the fixed sort key for a user's single preferences item;
	// notifLogPrefix prefixes one item per delivered notification (dedupe key).
	notifPrefsSK   = "NOTIFPREFS"
	notifLogPrefix = "NOTIFLOG#"

	// insightSKPrefix prefixes one snapshot item per day per user.
	insightSKPrefix = "INSIGHT#"

	// gsi2IndexName is the GSI (hash: GSI2PK, range: GSI2SK, declared in
	// infra/modules/api_gateway_lambda/main.tf) that ListEntries queries by
	// effectiveDate. Only entryItem sets GSI2PK/GSI2SK, so this index never
	// contains goal or category items.
	gsi2IndexName = "GSI2-Status"

	// gsi1IndexName is the GSI (hash: GSI1PK, range: GSI1SK) that ListEntries
	// queries by CashDate — the entradas/saídas de caixa basis.
	//
	// The physical name still says "Category" because that is what the index
	// held before: GSI1SK used to be "<category>#<date>", written on every
	// entry and queried by nothing (category filtering has always gone through
	// a filter expression on GSI2). Renaming it in Terraform would make
	// DynamoDB drop and rebuild the index for a label, so the stale name stays,
	// documented — the same trade-off goalItem makes for its "RevenueTarget"
	// attribute. Keep this constant in sync with the index name in
	// infra/modules/api_gateway_lambda/main.tf and with the fake's registration
	// in dynamodb_store_test.go; a mismatch is a ValidationException in
	// production that no test can see.
	gsi1IndexName = "GSI1-Category"
)

// DynamoDBStore implements Store using AWS DynamoDB.
// All financial data lives in a single table (single-table design).
type DynamoDBStore struct {
	client    dynamostore.API
	tableName string
}

var _ Store = (*DynamoDBStore)(nil)

// NewDynamoDBStore creates a DynamoDBStore. If endpoint is non-empty it
// overrides the endpoint (used for DynamoDB Local in docker-compose).
func NewDynamoDBStore(ctx context.Context, tableName, endpoint string) (*DynamoDBStore, error) {
	client, err := dynamostore.NewClient(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return NewDynamoDBStoreWithClient(client, tableName), nil
}

// NewDynamoDBStoreWithClient builds a store over any dynamostore.API. Tests use
// it to run the real store logic against an in-memory table.
func NewDynamoDBStoreWithClient(client dynamostore.API, tableName string) *DynamoDBStore {
	return &DynamoDBStore{client: client, tableName: tableName}
}
