package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/tools"
)

type whatsappWebhookRequest struct {
	UserID    string `json:"user_id"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Signature string `json:"signature"`
}

type webhookResponse struct {
	Message string `json:"message"`
}

func main() {
	stores := memory.NewInMemoryStores()
	_ = stores.Save(context.Background(), domain.Memory{
		UserID: "demo-user",
		Type:   "Preference",
		ID:     "Language",
		Value:  "pt-BR",
	})

	service := orchestrator.NewService(
		llm.StaticClient{},
		stores,
		stores,
		tools.NewRegistry(tools.EchoTool{}),
	)

	addr := shared.Getenv("WEBHOOK_ADDR", ":8080")
	secret := shared.Getenv("WEBHOOK_SECRET", "local-secret")

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req whatsappWebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		if !validSignature(req.Signature, secret) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		message, err := normalize(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		response, err := service.HandleMessage(r.Context(), message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(webhookResponse{Message: response.Text}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	log.Printf("webhook listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func normalize(req whatsappWebhookRequest) (domain.Message, error) {
	timestamp := time.Now().UTC()
	if strings.TrimSpace(req.Timestamp) != "" {
		parsed, err := time.Parse(time.RFC3339, req.Timestamp)
		if err != nil {
			return domain.Message{}, err
		}
		timestamp = parsed
	}

	return domain.Message{
		UserID:    strings.TrimSpace(req.UserID),
		Text:      strings.TrimSpace(req.Text),
		Timestamp: timestamp,
		MessageID: strings.TrimSpace(req.MessageID),
	}, nil
}

func validSignature(signature, secret string) bool {
	return strings.TrimSpace(signature) != "" && signature == secret
}

