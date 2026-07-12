package main

import (
	"context"
	"log"
	"net/http"

	"github.com/emerson/emerbot/apps/dashboard-api/internal/app"
	pkgauth "github.com/emerson/emerbot/packages/auth"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	ctx := context.Background()
	addr := shared.Getenv("API_ADDR", ":8081")
	jwtSecret := shared.Getenv("JWT_SECRET", "local-jwt-secret-change-in-prod")
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")

	var authStore pkgauth.Store
	var finStore pkgfinance.Store

	if endpoint != "" {
		usersTable := shared.Getenv("USERS_TABLE", "emerbot-local-users")
		tokensTable := shared.Getenv("REFRESH_TOKENS_TABLE", "emerbot-local-refresh-tokens")
		finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries")

		as, err := pkgauth.NewDynamoDBStore(ctx, usersTable, tokensTable, endpoint)
		if err != nil {
			log.Fatalf("auth store: %v", err)
		}
		fs, err := pkgfinance.NewDynamoDBStore(ctx, finTable, endpoint)
		if err != nil {
			log.Fatalf("finance store: %v", err)
		}
		authStore = as
		finStore = fs
	} else {
		log.Println("DYNAMODB_ENDPOINT not set — using in-memory stores")
		authStore = pkgauth.NewInMemoryStore()
		finStore = pkgfinance.NewInMemoryStore()
	}

	seedUsers(ctx, authStore)

	application := app.New(authStore, finStore, jwtSecret)
	log.Printf("dashboard-api listening on %s", addr)
	if err := http.ListenAndServe(addr, application); err != nil {
		log.Fatal(err)
	}
}
