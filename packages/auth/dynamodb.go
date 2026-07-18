package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/emerson/emerbot/packages/domain"
)

// emailIndexName is the GSI (hash: Email) declared on the users table in
// infra/opentofu/environments/dev/main.tf, used by GetUserByEmail.
const emailIndexName = "EmailIndex"

// DynamoDBStore implements Store using DynamoDB.
// Uses the users table (separate from the financial-entries table).
type DynamoDBStore struct {
	client      *dynamodb.Client
	usersTable  string
	tokensTable string
}

func NewDynamoDBStore(ctx context.Context, usersTable, tokensTable, endpoint string) (*DynamoDBStore, error) {
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
	return &DynamoDBStore{client: client, usersTable: usersTable, tokensTable: tokensTable}, nil
}

type userItem struct {
	PK           string `dynamodbav:"PK"`
	SK           string `dynamodbav:"SK"`
	UserID       string `dynamodbav:"UserID"`
	Email        string `dynamodbav:"Email"`
	PasswordHash string `dynamodbav:"PasswordHash"`
	Name         string `dynamodbav:"Name"`
}

type tokenItem struct {
	Token     string `dynamodbav:"Token"`
	UserID    string `dynamodbav:"UserID"`
	ExpiresAt string `dynamodbav:"ExpiresAt"`
	TTL       int64  `dynamodbav:"TTL"` // Unix timestamp for DynamoDB TTL
}

func (s *DynamoDBStore) SaveUser(ctx context.Context, user domain.User) error {
	item := userItem{
		PK:           "USER#" + user.UserID,
		SK:           "PROFILE",
		UserID:       user.UserID,
		Email:        user.Email,
		PasswordHash: user.PasswordHash,
		Name:         user.Name,
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.usersTable),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.usersTable),
		IndexName:              aws.String(emailIndexName),
		KeyConditionExpression: aws.String("Email = :email"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: email},
		},
	})
	if err != nil {
		return domain.User{}, err
	}
	if len(out.Items) == 0 {
		return domain.User{}, fmt.Errorf("user with email %q not found", email)
	}
	var item userItem
	if err := attributevalue.UnmarshalMap(out.Items[0], &item); err != nil {
		return domain.User{}, err
	}
	return domain.User{UserID: item.UserID, Email: item.Email, PasswordHash: item.PasswordHash, Name: item.Name}, nil
}

func (s *DynamoDBStore) GetUserByID(ctx context.Context, userID string) (domain.User, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.usersTable),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "USER#" + userID},
			"SK": &types.AttributeValueMemberS{Value: "PROFILE"},
		},
	})
	if err != nil {
		return domain.User{}, err
	}
	if out.Item == nil {
		return domain.User{}, fmt.Errorf("user %q not found", userID)
	}
	var item userItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return domain.User{}, err
	}
	return domain.User{UserID: item.UserID, Email: item.Email, PasswordHash: item.PasswordHash, Name: item.Name}, nil
}

func (s *DynamoDBStore) SaveRefreshToken(ctx context.Context, userID, token string, expiresAt time.Time) error {
	item := tokenItem{
		Token:     token,
		UserID:    userID,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		TTL:       expiresAt.Unix(),
	}
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tokensTable),
		Item:      av,
	})
	return err
}

func (s *DynamoDBStore) ValidateRefreshToken(ctx context.Context, token string) (string, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tokensTable),
		Key: map[string]types.AttributeValue{
			"Token": &types.AttributeValueMemberS{Value: token},
		},
	})
	if err != nil {
		return "", err
	}
	if out.Item == nil {
		return "", fmt.Errorf("refresh token not found")
	}
	var item tokenItem
	if err := attributevalue.UnmarshalMap(out.Item, &item); err != nil {
		return "", err
	}
	exp, err := time.Parse(time.RFC3339, item.ExpiresAt)
	if err != nil || time.Now().After(exp) {
		return "", fmt.Errorf("refresh token expired")
	}
	return item.UserID, nil
}

func (s *DynamoDBStore) RevokeRefreshToken(ctx context.Context, token string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tokensTable),
		Key: map[string]types.AttributeValue{
			"Token": &types.AttributeValueMemberS{Value: token},
		},
	})
	return err
}
