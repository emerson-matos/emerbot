package finance

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/emerson/emerbot/packages/domain"
)

type notifPrefsItem struct {
	PK             string `dynamodbav:"PK"`
	SK             string `dynamodbav:"SK"`
	UserID         string `dynamodbav:"UserID"`
	WAEnabled      bool   `dynamodbav:"WAEnabled"`
	Phone          string `dynamodbav:"Phone"`
	NotifyDueToday bool   `dynamodbav:"NotifyDueToday"`
	NotifyOverdue  bool   `dynamodbav:"NotifyOverdue"`
	NotifyGoal     bool   `dynamodbav:"NotifyGoal"`
}

func (s *DynamoDBStore) SaveNotificationPrefs(ctx context.Context, prefs domain.NotificationPrefs) error {
	item := notifPrefsItem{
		PK:             pkPrefix + prefs.UserID,
		SK:             notifPrefsSK,
		UserID:         prefs.UserID,
		WAEnabled:      prefs.WAEnabled,
		Phone:          prefs.Phone,
		NotifyDueToday: prefs.NotifyDueToday,
		NotifyOverdue:  prefs.NotifyOverdue,
		NotifyGoal:     prefs.NotifyGoal,
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal notif prefs: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) GetNotificationPrefs(ctx context.Context, userID string) (domain.NotificationPrefs, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK": &types.AttributeValueMemberS{Value: notifPrefsSK},
		},
	})
	if err != nil {
		return domain.NotificationPrefs{}, fmt.Errorf("get notif prefs: %w", err)
	}
	if out.Item == nil {
		return domain.NotificationPrefs{}, fmt.Errorf("notification prefs not found for %s", userID)
	}
	var item notifPrefsItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return domain.NotificationPrefs{}, fmt.Errorf("unmarshal notif prefs: %w", err)
	}
	return itemToNotifPrefs(item), nil
}

// ListNotificationPrefs scans for every user's preferences item. A Scan is
// acceptable here: this table holds one household's data and this runs once a
// day from the notifier, not on any user request path.
func (s *DynamoDBStore) ListNotificationPrefs(ctx context.Context) ([]domain.NotificationPrefs, error) {
	var result []domain.NotificationPrefs
	paginator := dynamodb.NewScanPaginator(s.client, &dynamodb.ScanInput{
		TableName:        aws.String(s.tableName),
		FilterExpression: aws.String("SK = :sk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sk": &types.AttributeValueMemberS{Value: notifPrefsSK},
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("scan notif prefs: %w", err)
		}
		for _, raw := range page.Items {
			var item notifPrefsItem
			if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
				continue
			}
			result = append(result, itemToNotifPrefs(item))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UserID < result[j].UserID })
	return result, nil
}

func itemToNotifPrefs(item notifPrefsItem) domain.NotificationPrefs {
	return domain.NotificationPrefs{
		UserID:         item.UserID,
		WAEnabled:      item.WAEnabled,
		Phone:          item.Phone,
		NotifyDueToday: item.NotifyDueToday,
		NotifyOverdue:  item.NotifyOverdue,
		NotifyGoal:     item.NotifyGoal,
	}
}

func (s *DynamoDBStore) NotificationSent(ctx context.Context, userID, key string) (bool, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK": &types.AttributeValueMemberS{Value: notifLogPrefix + key},
		},
	})
	if err != nil {
		return false, fmt.Errorf("get notif log: %w", err)
	}
	return out.Item != nil, nil
}

func (s *DynamoDBStore) RecordNotificationSent(ctx context.Context, userID, key string, sentAt time.Time) error {
	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item: map[string]types.AttributeValue{
			"PK":     &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK":     &types.AttributeValueMemberS{Value: notifLogPrefix + key},
			"UserID": &types.AttributeValueMemberS{Value: userID},
			"SentAt": &types.AttributeValueMemberS{Value: sentAt.UTC().Format(time.RFC3339)},
		},
	})
	return err
}
