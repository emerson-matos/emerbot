package wasession

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDBStore implements Store on a dedicated table whose hash key is Phone
// and whose TTL attribute is ExpiresAt (epoch seconds). DynamoDB physically
// removes expired items on its own schedule (which can lag hours), so Active
// also checks ExpiresAt at read time rather than trusting mere presence.
type DynamoDBStore struct {
	client    *dynamodb.Client
	tableName string
}

func NewDynamoDBStore(ctx context.Context, tableName, endpoint string) (*DynamoDBStore, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if endpoint != "" {
		opts = append(opts, awsconfig.WithBaseEndpoint(endpoint))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &DynamoDBStore{client: client, tableName: tableName}, nil
}

func (s *DynamoDBStore) RecordInbound(ctx context.Context, phone string, at time.Time) error {
	exp := strconv.FormatInt(at.Add(Window).Unix(), 10)
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item: map[string]types.AttributeValue{
			"Phone":     &types.AttributeValueMemberS{Value: phone},
			"ExpiresAt": &types.AttributeValueMemberN{Value: exp},
		},
		// Only ever extend the window; a delayed retry of an older message must
		// not move the expiry backwards.
		ConditionExpression: aws.String("attribute_not_exists(ExpiresAt) OR ExpiresAt < :exp"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":exp": &types.AttributeValueMemberN{Value: exp},
		},
	})
	if err != nil {
		var cond *types.ConditionalCheckFailedException
		if errors.As(err, &cond) {
			// A later expiry is already stored — not an error.
			return nil
		}
		return fmt.Errorf("record inbound: %w", err)
	}
	return nil
}

func (s *DynamoDBStore) MarkProcessed(ctx context.Context, messageID string, now time.Time) (bool, error) {
	if messageID == "" {
		return true, nil
	}
	exp := strconv.FormatInt(now.Add(DedupWindow).Unix(), 10)
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item: map[string]types.AttributeValue{
			"Phone":     &types.AttributeValueMemberS{Value: dedupKeyPrefix + messageID},
			"ExpiresAt": &types.AttributeValueMemberN{Value: exp},
		},
		// The write only succeeds the first time; a retry of the same message ID
		// fails the condition and is reported as a duplicate.
		ConditionExpression: aws.String("attribute_not_exists(Phone)"),
	})
	if err != nil {
		var cond *types.ConditionalCheckFailedException
		if errors.As(err, &cond) {
			return false, nil
		}
		return false, fmt.Errorf("mark processed: %w", err)
	}
	return true, nil
}

func (s *DynamoDBStore) Active(ctx context.Context, phone string, now time.Time) (bool, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"Phone": &types.AttributeValueMemberS{Value: phone},
		},
	})
	if err != nil {
		return false, fmt.Errorf("get session: %w", err)
	}
	if out.Item == nil {
		return false, nil
	}
	raw, ok := out.Item["ExpiresAt"].(*types.AttributeValueMemberN)
	if !ok {
		return false, nil
	}
	exp, err := strconv.ParseInt(raw.Value, 10, 64)
	if err != nil {
		return false, nil
	}
	// Read-time guard against TTL deletion lag: trust ExpiresAt, not presence.
	return time.Unix(exp, 0).After(now), nil
}
