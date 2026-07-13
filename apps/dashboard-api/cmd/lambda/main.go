package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/emerson/emerbot/apps/dashboard-api/internal/app"
	pkgauth "github.com/emerson/emerbot/packages/auth"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	ctx := context.Background()

	jwtSecret := shared.Getenv("JWT_SECRET", "")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}

	usersTable := shared.Getenv("USERS_TABLE", "")
	tokensTable := shared.Getenv("REFRESH_TOKENS_TABLE", "")
	finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "")

	authStore, err := pkgauth.NewDynamoDBStore(ctx, usersTable, tokensTable, "")
	if err != nil {
		log.Fatalf("auth store: %v", err)
	}
	finStore, err := pkgfinance.NewDynamoDBStore(ctx, finTable, "")
	if err != nil {
		log.Fatalf("finance store: %v", err)
	}

	application := app.New(authStore, finStore, jwtSecret)
	lambda.Start(application.HandleLambda)
}
