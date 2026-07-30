package finance

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/emerson/emerbot/packages/domain"
)

// Goals and categories: the per-user reference data that entries are recorded
// against. Neither carries GSI2 attributes, so neither ever shows up in a
// ListEntries query.

type goalItem struct {
	PK     string `dynamodbav:"PK"`
	SK     string `dynamodbav:"SK"`
	UserID string `dynamodbav:"UserID"`
	Month  string `dynamodbav:"Month"`
	// Go field and stored attribute agree again: the goal's target is a
	// faturamento target (see domain.Goal), which is what "Revenue" means
	// throughout this codebase. It was briefly called IncomeTarget in Go while
	// the tag stayed "RevenueTarget"; that split is gone.
	RevenueTarget int64 `dynamodbav:"RevenueTarget"`
	ExpenseTarget int64 `dynamodbav:"ExpenseTarget"`
}

type categoryItem struct {
	PK      string `dynamodbav:"PK"`
	SK      string `dynamodbav:"SK"`
	UserID  string `dynamodbav:"UserID"`
	Slug    string `dynamodbav:"Slug"`
	Label   string `dynamodbav:"Label"`
	Type    string `dynamodbav:"Type"`
	Default bool   `dynamodbav:"Default"`
}

func (s *DynamoDBStore) SaveGoal(ctx context.Context, goal domain.Goal) error {
	item := goalItem{
		PK:            pkPrefix + goal.UserID,
		SK:            goalPrefix + goal.Month,
		UserID:        goal.UserID,
		Month:         goal.Month,
		RevenueTarget: goal.RevenueTarget,
		ExpenseTarget: goal.ExpenseTarget,
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal goal: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) GetGoal(ctx context.Context, userID, month string) (domain.Goal, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkPrefix + userID},
			"SK": &types.AttributeValueMemberS{Value: goalPrefix + month},
		},
	})
	if err != nil {
		return domain.Goal{}, fmt.Errorf("get goal: %w", err)
	}
	if out.Item == nil {
		return domain.Goal{}, fmt.Errorf("goal not found for %s/%s", userID, month)
	}
	var item goalItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return domain.Goal{}, fmt.Errorf("unmarshal goal: %w", err)
	}
	return domain.Goal{
		UserID:        item.UserID,
		Month:         item.Month,
		RevenueTarget: item.RevenueTarget,
		ExpenseTarget: item.ExpenseTarget,
	}, nil
}

func (s *DynamoDBStore) SaveCategory(ctx context.Context, cat domain.Category) error {
	item := categoryItem{
		PK:      pkPrefix + cat.UserID,
		SK:      catPrefix + cat.Slug,
		UserID:  cat.UserID,
		Slug:    cat.Slug,
		Label:   cat.Label,
		Type:    string(cat.Type),
		Default: cat.Default,
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal category: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) ListCategories(ctx context.Context, userID string) ([]domain.Category, error) {
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: pkPrefix + userID},
			":prefix": &types.AttributeValueMemberS{Value: catPrefix},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}

	var cats []domain.Category
	for _, raw := range out.Items {
		var item categoryItem
		if err := attributevalue.UnmarshalMap(raw, &item); err != nil {
			continue
		}
		cats = append(cats, domain.Category{
			UserID:  item.UserID,
			Slug:    item.Slug,
			Label:   item.Label,
			Type:    domain.EntryType(item.Type),
			Default: item.Default,
		})
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i].Slug < cats[j].Slug })
	return cats, nil
}
