package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/emerson/emerbot/apps/webhook/internal/app"
	"github.com/emerson/emerbot/packages/shared"
)

// waWebhook matches the real WhatsApp Business Platform webhook payload.
type waWebhook struct {
	Object string    `json:"object"`
	Entry  []waEntry `json:"entry"`
}

type waEntry struct {
	ID      string     `json:"id"`
	Changes []waChange `json:"changes"`
}

type waChange struct {
	Value waValue `json:"value"`
	Field string  `json:"field"`
}

type waValue struct {
	MessagingProduct string      `json:"messaging_product"`
	Metadata         waMetadata  `json:"metadata"`
	Contacts         []waContact `json:"contacts"`
	Messages         []waMessage `json:"messages"`
}

type waMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type waContact struct {
	Profile waProfile `json:"profile"`
	WaID    string    `json:"wa_id"`
}

type waProfile struct {
	Name string `json:"name"`
}

type waMessage struct {
	From      string     `json:"from"`
	ID        string     `json:"id"`
	Timestamp string     `json:"timestamp"`
	Type      string     `json:"type"`
	Text      waTextBody `json:"text"`
}

type waTextBody struct {
	Body string `json:"body"`
}

func main() {
	addr := shared.Getenv("WEBHOOK_ADDR", ":8080")
	secret := shared.Getenv("WEBHOOK_SECRET", "local-secret")

	application := app.NewFromEnv(secret, "")

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		request, err := parseWebhookRequest(r, secret)
		if err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}

		response, status, err := application.Handle(r.Context(), request)
		if err != nil {
			log.Printf("handle error: %v", err)
			w.WriteHeader(status)
			return
		}

		w.WriteHeader(http.StatusOK)

		if response.Message != "" {
			log.Printf("reply to %s: %s", request.UserID, response.Message)
		}
	})

	log.Printf("local webhook listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func parseWebhookRequest(r *http.Request, secret string) (app.Request, error) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return app.Request{}, err
	}

	body, err := json.Marshal(raw)
	if err != nil {
		return app.Request{}, err
	}

	if _, ok := raw["object"]; ok {
		return fromWAWebhook(body, secret)
	}

	var legacy app.Request
	if err := json.Unmarshal(body, &legacy); err != nil {
		return app.Request{}, err
	}
	if legacy.Signature == "" {
		legacy.Signature = secret
	}
	return legacy, nil
}

func fromWAWebhook(body []byte, secret string) (app.Request, error) {
	var wa waWebhook
	if err := json.Unmarshal(body, &wa); err != nil {
		return app.Request{}, err
	}

	req := app.Request{
		MessageID: "unknown",
		Signature: secret,
	}
	if len(wa.Entry) == 0 || len(wa.Entry[0].Changes) == 0 {
		return req, nil
	}
	val := wa.Entry[0].Changes[0].Value
	req.PhoneNumberID = val.Metadata.PhoneNumberID
	if len(val.Contacts) > 0 {
		req.UserID = val.Contacts[0].WaID
	}
	if len(val.Messages) > 0 {
		req.MessageID = val.Messages[0].ID
		req.UserID = val.Messages[0].From
		req.Timestamp = waTimestamp(val.Messages[0].Timestamp)
		req.Text = val.Messages[0].Text.Body
	}
	return req, nil
}

func waTimestamp(ts string) string {
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ts
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}
