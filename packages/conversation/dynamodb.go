// Package conversation persists the short-term chat history the orchestrator
// threads into the LLM prompt, so the bot keeps context across messages and
// across Lambda cold starts (the previous in-memory store lost it on every
// container recycle). It implements orchestrator.ShortTermStore.
//
// Layout: one item per turn in a dedicated table (ADR-005 "Messages"), hash key
// PK = user id (WhatsApp phone) and a chronological, unique range key SK, so
// LoadRecent is a single bounded Query. Items self-expire via a TTL on ExpiresAt
// (epoch seconds); the recall window is bounded by LoadRecent's limit, not the
// TTL, which only handles cleanup.
package conversation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/emerson/emerbot/packages/domain"
)

// Retention is how long a turn is kept before DynamoDB's TTL removes it. It is
// only a cleanup bound — how much history reaches the model is set by the limit
// passed to LoadRecent, not by this value.
const Retention = 7 * 24 * time.Hour

// DynamoDBStore implements orchestrator.ShortTermStore over a dedicated table
// keyed by PK (user id) and SK (a chronological, unique sort key).
type DynamoDBStore struct {
	client    *dynamodb.Client
	tableName string
	now       func() time.Time
}

// item is one persisted conversation turn.
type item struct {
	PK        string `dynamodbav:"PK"`
	SK        string `dynamodbav:"SK"`
	Role      string `dynamodbav:"Role"`
	Text      string `dynamodbav:"Text"`
	Timestamp string `dynamodbav:"Timestamp"` // RFC3339, the turn's own time
	ExpiresAt int64  `dynamodbav:"ExpiresAt"` // epoch seconds, DynamoDB TTL
}

// NewDynamoDBStore creates a DynamoDBStore. If endpoint is non-empty it
// overrides the endpoint (used for DynamoDB Local in docker-compose).
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
	return &DynamoDBStore{client: client, tableName: tableName, now: time.Now}, nil
}

// Append records one turn. The sort key is derived from the server-side append
// time (not the message timestamp, which for inbound WhatsApp messages has only
// second granularity and could collide), plus a random suffix, so turns order
// by arrival and can never overwrite one another.
func (s *DynamoDBStore) Append(ctx context.Context, userID string, message domain.ConversationMessage) error {
	now := s.now().UTC()
	av, err := attributevalue.MarshalMap(item{
		PK:        userID,
		SK:        sortKey(now),
		Role:      string(message.Role),
		Text:      message.Text,
		Timestamp: message.Timestamp.UTC().Format(time.RFC3339),
		ExpiresAt: now.Add(Retention).Unix(),
	})
	if err != nil {
		return fmt.Errorf("marshal conversation turn: %w", err)
	}
	if _, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	}); err != nil {
		return fmt.Errorf("append conversation turn: %w", err)
	}
	return nil
}

// LoadRecent returns the last `limit` turns in chronological order (oldest
// first), matching the in-memory store's contract. It reads with ConsistentRead
// so a turn appended earlier in the same request is always visible.
func (s *DynamoDBStore) LoadRecent(ctx context.Context, userID string, limit int) ([]domain.ConversationMessage, error) {
	in := &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: userID},
		},
		// Newest-first so Limit keeps the most recent turns; reversed below.
		ScanIndexForward: aws.Bool(false),
		ConsistentRead:   aws.Bool(true),
	}
	if limit > 0 {
		in.Limit = aws.Int32(int32(limit))
	}

	out, err := s.client.Query(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("load recent conversation: %w", err)
	}

	// out.Items is newest-first (ScanIndexForward=false) in authoritative SK
	// order — the SK (append time), not the per-turn Timestamp, is the source of
	// truth for order.
	messages := make([]domain.ConversationMessage, 0, len(out.Items))
	for _, raw := range out.Items {
		var it item
		if err := attributevalue.UnmarshalMap(raw, &it); err != nil {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, it.Timestamp)
		messages = append(messages, domain.ConversationMessage{
			Role:      domain.Role(it.Role),
			Text:      it.Text,
			Timestamp: ts,
		})
	}
	// Reverse into oldest-first, matching the in-memory store's contract.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// sortKey builds a lexicographically chronological, collision-free range key:
// zero-padded epoch nanoseconds (fixed 19-digit width so string order matches
// numeric order) plus a random suffix to break ties within the same nanosecond.
func sortKey(t time.Time) string {
	return fmt.Sprintf("%019d#%s", t.UnixNano(), randomSuffix())
}

func randomSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is effectively fatal; fall back to a nanosecond
		// tail so the key stays unique enough rather than empty.
		return strconv.FormatInt(time.Now().UnixNano()%1e9, 10)
	}
	return hex.EncodeToString(b[:])
}
