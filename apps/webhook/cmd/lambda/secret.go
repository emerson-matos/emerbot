package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/emerson/emerbot/packages/shared"
)

func loadWebhookSecret(ctx context.Context) (string, error) {
	return loadParameter(ctx, "WEBHOOK_SECRET_PARAMETER", true)
}

func loadMetaToken(ctx context.Context) (string, error) {
	return loadParameter(ctx, "META_GRAPH_API_TOKEN_PARAMETER", false)
}

func loadParameter(ctx context.Context, envVar string, required bool) (string, error) {
	name := shared.Getenv(envVar, "")
	if name == "" {
		if required {
			return "", fmt.Errorf("%s is required", envVar)
		}
		return "", nil
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("load aws config: %w", err)
	}

	client := ssm.NewFromConfig(cfg)
	response, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("get parameter %q: %w", name, err)
	}
	if response.Parameter == nil || response.Parameter.Value == nil || *response.Parameter.Value == "" {
		return "", fmt.Errorf("parameter %q is empty", name)
	}

	return *response.Parameter.Value, nil
}
