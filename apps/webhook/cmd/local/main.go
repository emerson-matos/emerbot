package main

import (
	"io"
	"log"
	"net/http"

	"github.com/emerson/emerbot/apps/webhook/internal/app"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	addr := shared.Getenv("WEBHOOK_ADDR", ":8080")
	secret := shared.Getenv("WEBHOOK_SECRET", "local-secret")

	application := app.NewFromEnv(secret, "")

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
