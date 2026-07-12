package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/emerson/emerbot/packages/shared"
)

func loadWebhookSecret(ctx context.Context) (string, error) {
	secretID := shared.Getenv("WEBHOOK_SECRET_SECRET_ID", "")
	if secretID == "" {
		return "", fmt.Errorf("WEBHOOK_SECRET_SECRET_ID is required")
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}

	client := secretsmanager.NewFromConfig(cfg)
	response, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return "", fmt.Errorf("get secret value: %w", err)
	}
	if response.SecretString == nil || *response.SecretString == "" {
		return "", fmt.Errorf("secret %q is empty", secretID)
	}

	return *response.SecretString, nil
}
