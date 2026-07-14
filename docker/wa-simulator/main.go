package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	webhookURL    = getenv("WEBHOOK_URL", "http://localhost:8080/webhook")
	webhookSecret = getenv("WEBHOOK_SECRET", "local-secret")
)

// replyStore holds bot replies keyed by message ID.
type replyStore struct {
	mu   sync.Mutex
	repl map[string]string
}

func (s *replyStore) set(msgID, reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repl[msgID] = reply
}

func (s *replyStore) get(msgID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.repl[msgID]
	return r, ok
}

var replies = &replyStore{repl: make(map[string]string)}

type sendRequest struct {
	UserID string `json:"user_id"`
	Text   string `json:"text"`
}

type sendResult struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
	Error      string `json:"error,omitempty"`
}

type waWebhookPayload struct {
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

// replyPayload is what the webhook POSTs back to simulate Meta delivering the reply.
type replyPayload struct {
	To      string `json:"to"`
	Message string `json:"message"`
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /", serveUI)
	mux.HandleFunc("POST /send", handleSend)
	mux.HandleFunc("POST /reply", handleReply)

	addr := getenv("ADDR", ":9000")
	log.Printf("wa-simulator listening on %s → forwarding to %s", addr, webhookURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.UserID == "" {
		req.UserID = "pai"
	}

	msgID := send(req.UserID, req.Text)
	if msgID == "" {
		if err := json.NewEncoder(w).Encode(sendResult{Error: "failed to send"}); err != nil {
			log.Printf("encode error response: %v", err)
		}
		return
	}

	// Poll for the reply (simulates async delivery via Meta).
	reply := waitForReply(msgID, 8*time.Second)
	if err := json.NewEncoder(w).Encode(sendResult{StatusCode: 200, Body: reply}); err != nil {
		log.Printf("encode reply: %v", err)
	}
}

func send(userID, text string) string {
	now := time.Now().UTC()
	ts := strconv.FormatInt(now.Unix(), 10)
	msgID := fmt.Sprintf("wamid.sim.%d", now.UnixNano())

	payload := waWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []waEntry{{
			ID: "sim-account-id",
			Changes: []waChange{{
				Value: waValue{
					MessagingProduct: "whatsapp",
					Metadata: waMetadata{
						DisplayPhoneNumber: "15550783881",
						PhoneNumberID:      "sim-phone-id",
					},
					Contacts: []waContact{{
						Profile: waProfile{Name: userID},
						WaID:    userID,
					}},
					Messages: []waMessage{{
						From:      userID,
						ID:        msgID,
						Timestamp: ts,
						Type:      "text",
						Text:      waTextBody{Body: text},
					}},
				},
				Field: "messages",
			}},
		}},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("new webhook request: %v", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sign(body, webhookSecret))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("send: %v", err)
		return ""
	}
	if err := resp.Body.Close(); err != nil {
		log.Printf("close send response body: %v", err)
	}
	return msgID
}

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func waitForReply(msgID string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if reply, ok := replies.get(msgID); ok {
			return reply
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "(no reply received within timeout)"
}

// handleReply receives the bot's response as if delivered by Meta.
func handleReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p replyPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	replies.set(p.To, p.Message)
	w.WriteHeader(http.StatusOK)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var uiTmpl = template.Must(template.New("ui").Parse(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="UTF-8">
  <title>📱 WhatsApp Simulator — Farmácia</title>
  <style>
    * { box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; max-width: 640px; margin: 40px auto; padding: 0 20px; background: #f0f2f5; }
    .card { background: white; border-radius: 12px; padding: 24px; box-shadow: 0 1px 4px rgba(0,0,0,.1); margin-bottom: 16px; }
    h2 { margin: 0 0 4px; color: #128c7e; }
    .subtitle { color: #667781; font-size: 14px; margin-bottom: 20px; }
    label { display: block; font-size: 13px; font-weight: 600; color: #3b4a54; margin-bottom: 4px; }
    input, textarea { width: 100%; border: 1px solid #d1d7db; border-radius: 8px; padding: 10px 12px; font-size: 15px; margin-bottom: 12px; }
    textarea { resize: vertical; }
    button { width: 100%; padding: 12px; background: #25d366; color: white; border: none; border-radius: 8px; font-size: 16px; font-weight: 600; cursor: pointer; }
    button:hover { background: #1da851; }
    .examples { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }
    .chip { padding: 6px 12px; background: #e9f5ee; color: #128c7e; border-radius: 16px; font-size: 13px; cursor: pointer; border: 1px solid #c3e6cb; }
    .chip:hover { background: #c3e6cb; }
    pre { background: #f0f2f5; border-radius: 8px; padding: 12px; white-space: pre-wrap; word-break: break-all; font-size: 13px; min-height: 60px; }
    .status { font-size: 12px; color: #667781; margin-top: 8px; }
  </style>
</head>
<body>
  <div class="card">
    <h2>📱 WhatsApp Simulator</h2>
    <p class="subtitle">Simula mensagens do seu pai para o bot financeiro</p>

    <label>Usuário</label>
    <input id="userId" value="pai" />

    <label>Exemplos rápidos</label>
    <div class="examples">
      <span class="chip" onclick="fill('/resumo')">/resumo</span>
      <span class="chip" onclick="fill('/despesa 500 aluguel')">/despesa 500 aluguel</span>
      <span class="chip" onclick="fill('/despesa 500 aluguel 10/07')">/despesa 500 aluguel 10/07</span>
      <span class="chip" onclick="fill('/receita 1200 venda_balcao')">/receita 1200 venda_balcao</span>
      <span class="chip" onclick="fill('/pagar 300 energia_agua 20/07')">/pagar 300 energia_agua 20/07</span>
      <span class="chip" onclick="fill('/receber 800 convenio')">/receber 800 convenio</span>
      <span class="chip" onclick="fill('/recorrente pagar 350 aluguel mensal 12')">/recorrente pagar 350 aluguel mensal 12</span>
      <span class="chip" onclick="fill('/despesa 1500,50 fornecedor_medicamentos Distribuidora')">/despesa 1500,50 fornecedor</span>
      <span class="chip" onclick="fill('/goal')">/goal</span>
      <span class="chip" onclick="fill('/meta 80000 60000')">/meta 80000 60000</span>
    </div>

    <label>Mensagem</label>
    <textarea id="text" rows="3" placeholder="/despesa 500 aluguel"></textarea>

    <button onclick="send()">📤 Enviar Mensagem</button>
  </div>

  <div class="card">
    <label>Resposta do Bot</label>
    <pre id="out">—</pre>
    <p class="status" id="status"></p>
  </div>

  <script>
    function fill(text) {
      document.getElementById('text').value = text;
      document.getElementById('text').focus();
    }
    async function send() {
      const text = document.getElementById('text').value.trim();
      if (!text) return;
      document.getElementById('out').textContent = '...';
      document.getElementById('status').textContent = '';
      try {
        const res = await fetch('/send', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            user_id: document.getElementById('userId').value || 'pai',
            text,
          })
        });
        const data = await res.json();
        let reply = '—';
        try {
          const body = JSON.parse(data.body);
          reply = body.message || JSON.stringify(body, null, 2);
        } catch {
          reply = data.body || data.error || JSON.stringify(data, null, 2);
        }
        document.getElementById('out').textContent = reply;
        document.getElementById('status').textContent = 'HTTP ' + data.status_code + ' · ' + new Date().toLocaleTimeString('pt-BR');
      } catch(e) {
        document.getElementById('out').textContent = 'Erro: ' + e.message;
      }
    }
    document.getElementById('text').addEventListener('keydown', e => {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) send();
    });
  </script>
</body>
</html>`))

func serveUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := uiTmpl.Execute(w, nil); err != nil {
		log.Printf("execute UI template: %v", err)
	}
}
