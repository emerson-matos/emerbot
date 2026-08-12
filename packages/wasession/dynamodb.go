package wasession

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/dynamostore"
)

// DynamoDBStore implements Store on a dedicated table whose hash key is Phone
// and whose TTL attribute is ExpiresAt (epoch seconds). DynamoDB physically
// removes expired items on its own schedule (which can lag hours), so Active
// also checks ExpiresAt at read time rather than trusting mere presence.
type DynamoDBStore struct {
	client    dynamostore.API
	tableName string
}

var _ Store = (*DynamoDBStore)(nil)

func NewDynamoDBStore(ctx context.Context, tableName, endpoint string) (*DynamoDBStore, error) {
	client, err := dynamostore.NewClient(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return NewDynamoDBStoreWithClient(client, tableName), nil
}

// NewDynamoDBStoreWithClient builds a store over any dynamostore.API, which is
// how tests exercise the conditional writes this store relies on.
func NewDynamoDBStoreWithClient(client dynamostore.API, tableName string) *DynamoDBStore {
	return &DynamoDBStore{client: client, tableName: tableName}
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

func (s *DynamoDBStore) Processed(ctx context.Context, messageID string, now time.Time) (bool, error) {
	if messageID == "" {
		return false, nil
	}
	// Leitura eventualmente consistente de propósito, ao contrário do que faz o
	// caderninho (packages/fiado), onde duas mensagens chegam com segundos de
	// diferença. Aqui a segunda entrega da mesma mensagem só acontece depois do
	// visibility timeout da fila — 360s — e a marca é escrita pela entrega
	// anterior, no mesmo grupo FIFO, que o SQS não deixa correr em paralelo.
	// A janela de inconsistência do DynamoDB é de milissegundos: não há corrida
	// aqui, e uma leitura consistente custaria o dobro sem comprar nada.
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"Phone": &types.AttributeValueMemberS{Value: dedupKeyPrefix + messageID},
		},
	})
	if err != nil {
		return false, fmt.Errorf("processed: %w", err)
	}
	if out.Item == nil {
		return false, nil
	}
	raw, ok := out.Item["ExpiresAt"].(*types.AttributeValueMemberN)
	if !ok {
		// The mark is there; only its expiry is unreadable. A mark is a fact
		// about a message that was answered, and answering somebody twice is
		// worse than letting one duplicate through.
		return true, nil
	}
	exp, err := strconv.ParseInt(raw.Value, 10, 64)
	if err != nil {
		return true, nil
	}
	// Same read-time guard as Active: TTL deletion can lag by hours, so trust
	// ExpiresAt rather than mere presence.
	return time.Unix(exp, 0).After(now), nil
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
		// The write only succeeds the first time; a redelivery of the same
		// message ID fails the condition and is reported as a duplicate. This
		// is the conditional write ADR-029 rests on: whoever writes, processed.
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

func (s *DynamoDBStore) ActiveUntil(ctx context.Context, phone string) (time.Time, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"Phone": &types.AttributeValueMemberS{Value: phone},
		},
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("get session: %w", err)
	}
	if out.Item == nil {
		return time.Time{}, nil
	}
	raw, ok := out.Item["ExpiresAt"].(*types.AttributeValueMemberN)
	if !ok {
		return time.Time{}, nil
	}
	exp, err := strconv.ParseInt(raw.Value, 10, 64)
	if err != nil {
		return time.Time{}, nil
	}
	return time.Unix(exp, 0).UTC(), nil
}

func (s *DynamoDBStore) Active(ctx context.Context, phone string, now time.Time) (bool, error) {
	exp, err := s.ActiveUntil(ctx, phone)
	if err != nil {
		return false, err
	}
	// Read-time guard against TTL deletion lag: trust ExpiresAt, not presence.
	return !exp.IsZero() && exp.After(now), nil
}
