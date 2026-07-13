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
	return loadSecret(ctx, "WEBHOOK_SECRET_SECRET_ID", true)
}

func loadMetaToken(ctx context.Context) (string, error) {
	return loadSecret(ctx, "META_GRAPH_API_TOKEN_SECRET_ID", false)
}

func loadSecret(ctx context.Context, envVar string, required bool) (string, error) {
	secretID := shared.Getenv(envVar, "")
	if secretID == "" {
		if required {
			return "", fmt.Errorf("%s is required", envVar)
		}
		return "", nil
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
		return "", fmt.Errorf("get secret %q: %w", secretID, err)
	}
	if response.SecretString == nil || *response.SecretString == "" {
		return "", fmt.Errorf("secret %q is empty", secretID)
	}

	return *response.SecretString, nil
}
