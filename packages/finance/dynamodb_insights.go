package finance

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const insightTTLDays = 30

type insightItem struct {
	PK         string `dynamodbav:"PK"`
	SK         string `dynamodbav:"SK"`
	Snapshot   []byte `dynamodbav:"Snapshot"`
	ComputedAt string `dynamodbav:"ComputedAt"` // RFC3339
	TTL        int64  `dynamodbav:"TTL"`        // epoch seconds
}

func (s *DynamoDBStore) SaveInsightSnapshot(ctx context.Context, userID, date string, snapshot []byte, computedAt time.Time) error {
	item := insightItem{
		PK:         pkPrefix + userID,
		SK:         insightSKPrefix + date,
		Snapshot:   snapshot,
		ComputedAt: computedAt.UTC().Format(time.RFC3339),
		TTL:        computedAt.UTC().Add(insightTTLDays * 24 * time.Hour).Unix(),
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal insight snapshot: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) GetInsightSnapshot(ctx context.Context, userID, date string) (InsightSnapshot, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK": &types.AttributeValueMemberS{Value: insightSKPrefix + date},
		},
	})
	if err != nil {
		return InsightSnapshot{}, fmt.Errorf("get insight snapshot: %w", err)
	}
	if out.Item == nil {
		return InsightSnapshot{}, fmt.Errorf("insight snapshot not found for %s/%s", userID, date)
	}
	var item insightItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return InsightSnapshot{}, fmt.Errorf("unmarshal insight snapshot: %w", err)
	}
	computedAt, err := time.Parse(time.RFC3339, item.ComputedAt)
	if err != nil {
		return InsightSnapshot{}, fmt.Errorf("parse computed at: %w", err)
	}
	return InsightSnapshot{
		Snapshot:   item.Snapshot,
		ComputedAt: computedAt,
	}, nil
}
