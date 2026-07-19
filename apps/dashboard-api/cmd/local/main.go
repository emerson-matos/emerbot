package main

import (
	"context"
	"log"
	"net/http"

	"github.com/emerson/emerbot/apps/dashboard-api/internal/app"
	apiauth "github.com/emerson/emerbot/apps/dashboard-api/internal/auth"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	ctx := context.Background()
	addr := shared.Getenv("API_ADDR", ":8081")
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")

	var finStore pkgfinance.Store

	if endpoint != "" {
		finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries")

		fs, err := pkgfinance.NewDynamoDBStore(ctx, finTable, endpoint)
		if err != nil {
			log.Fatalf("finance store: %v", err)
		}
		finStore = fs
	} else {
		log.Println("DYNAMODB_ENDPOINT not set — using in-memory store")
		finStore = pkgfinance.NewInMemoryStore()
	}

	// cognito-local generates its own pool/client IDs at creation time (see
	// docker/cognito-init/init.sh), so unlike other env vars here these have no
	// sensible hardcoded default — they must come from the environment. Under
	// podman compose, dashboard-api's entrypoint wrapper sources them from the
	// file cognito-init writes; for a bare `go run` outside compose, run
	// cognito-init once and export the same three vars yourself.
	jwksURL := shared.Getenv("COGNITO_JWKS_URL", "")
	issuer := shared.Getenv("COGNITO_ISSUER", "")
	clientID := shared.Getenv("COGNITO_CLIENT_ID", "")
	if jwksURL == "" || issuer == "" || clientID == "" {
		log.Fatal("COGNITO_JWKS_URL, COGNITO_ISSUER and COGNITO_CLIENT_ID are required " +
			"(is cognito-local up? try `podman compose up cognito-local cognito-init -d`)")
	}

	authMw, err := apiauth.NewLocalCognitoMiddleware(ctx, jwksURL, issuer, clientID)
	if err != nil {
		log.Fatalf("cognito JWKS setup failed (is cognito-local up? try `podman compose up cognito-local cognito-init -d`): %v", err)
	}

	application := app.NewLocal(finStore, authMw)
	log.Printf("dashboard-api listening on %s", addr)
	if err := http.ListenAndServe(addr, application); err != nil {
		log.Fatal(err)
	}
}
