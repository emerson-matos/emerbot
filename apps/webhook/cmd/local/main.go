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
		if r.Method == http.MethodGet {
			mode := r.URL.Query().Get("hub.mode")
			token := r.URL.Query().Get("hub.verify_token")
			challenge := r.URL.Query().Get("hub.challenge")
			resp := application.HandleVerification(mode, token, challenge)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(resp.StatusCode)
			if _, err := w.Write([]byte(resp.Body)); err != nil {
				log.Printf("write verification response: %v", err)
			}
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		req, _ := app.FromWAWebhook(body)
		_, status, err := application.Handle(r.Context(), *req)
		if err != nil {
			log.Printf("handle error: %v", err)
			w.WriteHeader(status)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	log.Printf("local webhook listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
