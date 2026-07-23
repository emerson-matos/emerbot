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
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>WhatsApp Simulator — Farmácia Financeira</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
      background: #f0f2f5; color: #111b21;
      display: flex; justify-content: center; min-height: 100vh; padding: 24px;
    }
    .app {
      max-width: 720px; width: 100%;
      background: white; border-radius: 12px; box-shadow: 0 1px 4px rgba(0,0,0,.1);
      display: flex; flex-direction: column; height: calc(100vh - 48px); max-height: 800px;
    }
    .header { padding: 20px 24px 12px; border-bottom: 1px solid #e9edef; flex-shrink: 0; }
    .header h1 { font-size: 20px; color: #128c7e; }
    .header p { font-size: 13px; color: #667781; margin-top: 2px; }

    .history {
      flex: 1; overflow-y: auto; padding: 12px 24px;
      display: flex; flex-direction: column; gap: 4px;
    }
    .history:empty::after {
      content: 'Nenhuma mensagem ainda — envie algo acima.';
      display: block; text-align: center; color: #8696a0; font-size: 14px;
      margin: auto;
    }
    .entry { padding: 8px 12px; border-left: 3px solid #25d366; margin: 2px 0; }
    .entry.bot { border-left-color: #128c7e; }
    .entry .meta { font-size: 11px; color: #8696a0; margin-bottom: 2px; }
    .entry .meta .role { font-weight: 600; }
    .entry .meta .time { float: right; }
    .entry .text {
      font-size: 14px; line-height: 1.5; white-space: pre-wrap; word-break: break-word;
    }
    .entry .text.error { color: #dc3545; }
    .entry .text.loading { color: #8696a0; font-style: italic; }

    .input-area { padding: 12px 24px 20px; border-top: 1px solid #e9edef; flex-shrink: 0; }
    .input-row { display: flex; gap: 8px; align-items: flex-start; }
    .input-row input,
    .input-row textarea {
      flex: 1; border: 1px solid #d1d7db; border-radius: 8px; padding: 10px 12px;
      font-size: 15px; font-family: inherit; resize: none;
    }
    .input-row textarea { min-height: 44px; max-height: 120px; }
    .input-row button {
      padding: 10px 20px; background: #25d366; color: white; border: none;
      border-radius: 8px; font-size: 15px; font-weight: 600; cursor: pointer;
      white-space: nowrap; margin-top: 0;
    }
    .input-row button:hover { background: #1da851; }
    .input-row button:disabled { opacity: .5; cursor: not-allowed; }

    .user-row { display: flex; gap: 8px; align-items: center; margin-bottom: 8px; }
    .user-row label { font-size: 13px; font-weight: 600; color: #3b4a54; }
    .user-row input {
      flex: 1; border: 1px solid #d1d7db; border-radius: 8px; padding: 6px 10px;
      font-size: 14px; max-width: 120px;
    }

    .chips { display: flex; flex-wrap: wrap; gap: 6px; margin: 8px 0; }
    .chip {
      padding: 4px 10px; background: #e9f5ee; color: #128c7e;
      border-radius: 12px; font-size: 12px; cursor: pointer; border: 1px solid #c3e6cb;
      white-space: nowrap;
    }
    .chip:hover { background: #c3e6cb; }
  </style>
</head>
<body>
  <div class="app">
    <div class="header">
      <h1>📱 WhatsApp Simulator</h1>
      <p>Simula o pai mandando mensagem pro bot financeiro</p>
    </div>

    <div class="history" id="history"></div>

    <div class="input-area">
      <div class="user-row">
        <label for="userId">Usuário</label>
        <input id="userId" value="pai" />
      </div>

      <div class="chips">
        <span class="chip" onclick="fill('/resumo')">/resumo</span>
        <span class="chip" onclick="fill('/despesa 500 aluguel')">/despesa 500 aluguel</span>
        <span class="chip" onclick="fill('/despesa 500 aluguel 10/07')">/despesa 500 aluguel 10/07</span>
        <span class="chip" onclick="fill('/receita 1200 venda_balcao')">/receita 1200 venda_balcao</span>
        <span class="chip" onclick="fill('/pagar 300 energia_agua 20/07')">/pagar 300 energia_agua 20/07</span>
        <span class="chip" onclick="fill('/receber 800 convenio')">/receber 800 convenio</span>
        <span class="chip" onclick="fill('/recorrente pagar 350 aluguel mensal 12')">/recorrente pagar 350 aluguel mensal 12</span>
        <span class="chip" onclick="fill('/goal')">/goal</span>
        <span class="chip" onclick="fill('/meta 80000 60000')">/meta 80000 60000</span>
      </div>

      <div class="input-row">
        <textarea id="text" rows="1" placeholder="Digite sua mensagem..." oninput="autoHeight(this)"></textarea>
        <button id="sendBtn" onclick="send()">📤 Enviar</button>
      </div>
    </div>
  </div>

  <script>
    const STORAGE_KEY = 'wa-chat-history';
    let messages = [];

    function load() {
      try {
        const raw = sessionStorage.getItem(STORAGE_KEY);
        if (raw) messages = JSON.parse(raw);
      } catch {}
    }

    function save() {
      try { sessionStorage.setItem(STORAGE_KEY, JSON.stringify(messages)); } catch {}
    }

    function render() {
      const el = document.getElementById('history');
      el.innerHTML = messages.map(m => {
        const cls = m.role === 'bot' ? 'entry bot' : 'entry';
        const textCls = m.error ? 'text error' : m.loading ? 'text loading' : 'text';
        return '<div class="' + cls + '">' +
          '<div class="meta"><span class="role">' + m.label + '</span><span class="time">' + m.time + '</span></div>' +
          '<div class="' + textCls + '">' + esc(m.text) + '</div>' +
          '</div>';
      }).join('');
      el.scrollTop = el.scrollHeight;
    }

    function esc(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

    function now() { return new Date().toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit', second: '2-digit' }); }

    function fill(text) {
      document.getElementById('text').value = text;
      autoHeight(document.getElementById('text'));
      document.getElementById('text').focus();
    }

    function autoHeight(el) {
      el.style.height = 'auto';
      el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    }

    async function send() {
      const textEl = document.getElementById('text');
      const btn = document.getElementById('sendBtn');
      const text = textEl.value.trim();
      if (!text || btn.disabled) return;

      const userId = document.getElementById('userId').value.trim() || 'pai';

      messages.push({ role: 'user', label: 'Você', text, time: now() });
      const loadingIdx = messages.length;
      messages.push({ role: 'bot', label: 'Bot', text: '...', time: now(), loading: true });
      textEl.value = ''; autoHeight(textEl); btn.disabled = true;
      render(); save();

      try {
        const res = await fetch('/send', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ user_id: userId, text })
        });
        const data = await res.json();

        let reply = '—';
        try {
          const body = JSON.parse(data.body);
          reply = body.message || JSON.stringify(body, null, 2);
        } catch {
          reply = data.body || data.error || JSON.stringify(data, null, 2);
        }

        messages[loadingIdx] = { role: 'bot', label: 'Bot', text: reply, time: now() };
      } catch(e) {
        messages[loadingIdx] = { role: 'bot', label: 'Bot', text: 'Erro: ' + e.message, time: now(), error: true };
      }

      btn.disabled = false; render(); save();
      textEl.focus();
    }

    document.getElementById('text').addEventListener('keydown', e => {
      if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
    });

    load(); render();
  </script>
</body>
</html>`))

func serveUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := uiTmpl.Execute(w, nil); err != nil {
		log.Printf("execute UI template: %v", err)
	}
}
