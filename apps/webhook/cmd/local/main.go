package main

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/emerson/emerbot/apps/webhook/internal/app"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wainbound"
	"github.com/emerson/emerbot/packages/waturn"
)

// inlinePublisher stands in for the queue when there is no queue.
//
// Locally (`make demo`, `make run-webhook`) there is no SQS and no second
// Lambda, so the webhook's publisher runs the worker's turn in the same
// process: the caller blocks for the length of the turn, exactly as the old
// webhook did, and the wa-simulator still gets its reply. Deployed, this code
// is never reached — apps/webhook/cmd/lambda refuses to start without a queue
// URL, so production always has the real thing.
//
// lastAttempt is true because it is: nothing here will redeliver, so a failed
// turn must tell the person rather than counting on a retry that is not coming.
type inlinePublisher struct{ turn *waturn.Worker }

func (p inlinePublisher) Publish(ctx context.Context, env wainbound.Envelope) error {
	return p.turn.Process(ctx, env, true)
}

func main() {
	shared.InitSlog()
	addr := shared.Getenv("WEBHOOK_ADDR", ":8080")
	secret := shared.Getenv("WEBHOOK_SECRET", "local-secret")

	application := app.NewFromEnvWithPublisher(secret, "",
		inlinePublisher{turn: waturn.NewFromEnv(context.Background())})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		if err := r.Body.Close(); err != nil {
			log.Printf("close request body: %v", err)
		}

		resp, err := application.HandleWebhookHTTP(r.Context(), app.WebhookHTTPRequest{
			Method: r.Method,
			Query: map[string]string{
				"hub.mode":         r.URL.Query().Get("hub.mode"),
				"hub.verify_token": r.URL.Query().Get("hub.verify_token"),
				"hub.challenge":    r.URL.Query().Get("hub.challenge"),
			},
			Header: flattenHeaders(r.Header),
			Body:   body,
		})
		if err != nil {
			log.Printf("handle webhook: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		for key, value := range resp.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(resp.StatusCode)
		if _, err := w.Write([]byte(resp.Body)); err != nil {
			log.Printf("write webhook response: %v", err)
		}
	})

	log.Printf("local webhook listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func flattenHeaders(headers http.Header) map[string]string {
	flat := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		flat[key] = values[0]
	}
	return flat
}
