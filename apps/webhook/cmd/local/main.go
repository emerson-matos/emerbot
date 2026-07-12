package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/emerson/emerbot/apps/webhook/internal/app"
	"github.com/emerson/emerbot/apps/webhook/internal/financial"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/tools"
	"github.com/emerson/emerbot/packages/whatsapp"
)

func main() {
	addr := shared.Getenv("WEBHOOK_ADDR", ":8080")
	secret := shared.Getenv("WEBHOOK_SECRET", "local-secret")
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")

	var application *app.App

	if endpoint != "" {
		finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries")
		ctx := context.Background()

		finStore, err := pkgfinance.NewDynamoDBStore(ctx, finTable, endpoint)
		if err != nil {
			log.Fatalf("finance store: %v", err)
		}

		parser := whatsapp.NewRegexParser()
		finHandler := financial.NewHandler(parser, finStore)

		stores := memory.NewInMemoryStores()
		svc := orchestrator.NewService(
			llm.StaticClient{},
			stores, stores,
			tools.NewRegistry(tools.EchoTool{}),
		)
		application = app.New(svc, finHandler, secret)
	} else {
		application = app.NewDefault(secret)
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var request app.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		response, status, err := application.Handle(r.Context(), request)
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	log.Printf("local webhook listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
