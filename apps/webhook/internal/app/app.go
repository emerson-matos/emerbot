package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/emerson/emerbot/packages/conversation"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wasession"
	"github.com/emerson/emerbot/packages/whatsapp"
)

type Request struct {
	UserID        string `json:"user_id"`
	MessageID     string `json:"message_id"`
	PhoneNumberID string `json:"phone_number_id"`
	Text          string `json:"text"`
	Timestamp     string `json:"timestamp"`
}

type Response struct {
	Message string `json:"message"`
}

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
	Statuses         []waStatus  `json:"statuses"`
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

type waStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// There is deliberately no slash-command layer here. Every message goes to the
// agent, which writes the ledger through the finance tools
// (packages/finance.FinanceTools) with typed arguments.
//
// The commands it replaced parsed amounts out of free text with a regex, and
// that regex read a pt-BR thousands separator as a decimal point: "/despesa
// 1.500 aluguel" saved R$ 1,50 under a category called "0", with no error. A
// second parser behind /meta stripped both separators and multiplied by 100, so
// "/meta 80.000,00" set a goal of R$ 8.000.000,00. Both failures were silent and
// both landed in the ledger, which is why the layer is gone rather than fixed:
// the tools take an amount as a number the model has already read, so there is
// no text left to misparse.

// SessionStore records when a phone last messaged us, so the scheduled
// notifier can respect WhatsApp's 24h customer-service window (free-form
// messages are only allowed within it). packages/wasession satisfies this.
type SessionStore interface {
	RecordInbound(ctx context.Context, phone string, at time.Time) error
	// MarkProcessed records that a message ID was handled and reports whether
	// this is the first time it was seen (false = a WhatsApp retry to ignore).
	MarkProcessed(ctx context.Context, messageID string, now time.Time) (bool, error)
	// Unmark drops a message's dedup marker so a WhatsApp retry is reprocessed
	// when the original turn ended without a 2xx.
	Unmark(ctx context.Context, messageID string) error
}

type App struct {
	service        *orchestrator.Service
	whatsappClient whatsapp.Client
	sessions       SessionStore
	secret         string
	verifyToken    string
}

func New(service *orchestrator.Service, waClient whatsapp.Client, secret, verifyToken string, sessions SessionStore) *App {
	if verifyToken == "" {
		verifyToken = secret
	}
	return &App{
		service:        service,
		whatsappClient: waClient,
		sessions:       sessions,
		secret:         secret,
		verifyToken:    verifyToken,
	}
}

func NewFromEnv(secret, graphAPIToken string) *App {
	var sessions SessionStore
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")

	cfg := orchestrator.Config{}
	finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "")
	if finTable != "" {
		ctx := context.Background()
		store, err := pkgfinance.NewDynamoDBStore(ctx, finTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: finance store: %v", err)
		}
		cfg.FinanceStore = store
		cfg.GeminiAPIKey = shared.Getenv("GEMINI_API_KEY", "")
		// LLM_PROVIDER=ollama runs a local open-source model for dev (ADR-012);
		// unset keeps the Gemini/static path used in production.
		cfg.LLMProvider = shared.Getenv("LLM_PROVIDER", "")
		cfg.OllamaHost = shared.Getenv("OLLAMA_HOST", "")
		cfg.OllamaModel = shared.Getenv("OLLAMA_MODEL", "")
		cfg.DashboardURL = shared.Getenv("DASHBOARD_URL", "")
	}

	// Short-term chat history lives in its own TTL-managed table so the bot keeps
	// context across messages and cold starts. When unset, NewService falls back
	// to an in-memory store (fine locally, lost on every Lambda recycle).
	if convTable := shared.Getenv("CONVERSATIONS_TABLE", ""); convTable != "" {
		ctx := context.Background()
		convStore, err := conversation.NewDynamoDBStore(ctx, convTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: conversation store: %v", err)
		}
		cfg.ShortTerm = convStore
	}

	// The 24h-window session store lives in its own table (TTL-managed), so it
	// is wired independently of the finance store.
	if sessTable := shared.Getenv("WHATSAPP_SESSIONS_TABLE", ""); sessTable != "" {
		ctx := context.Background()
		sessStore, err := wasession.NewDynamoDBStore(ctx, sessTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: session store: %v", err)
		}
		sessions = sessStore
	}

	svc := orchestrator.NewService(cfg)
	waClient := whatsapp.NewClientFromEnv(graphAPIToken)
	verifyToken := shared.Getenv("WEBHOOK_VERIFY_TOKEN", secret)

	return New(svc, waClient, secret, verifyToken, sessions)
}

func (a *App) Handle(ctx context.Context, req Request) (resp Response, status int, err error) {
	message, err := normalize(req)
	if err != nil {
		return Response{}, http.StatusBadRequest, err
	}

	if a.whatsappClient != nil {
		if err := a.whatsappClient.MarkAsRead(ctx, req.PhoneNumberID, req.MessageID); err != nil {
			log.Printf("mark as read: %v", err)
		}
	}

	// Any inbound message opens WhatsApp's 24h customer-service window, so
	// record it (best-effort) regardless of how the message routes below. The
	// scheduled notifier reads this to avoid sending outside the window.
	if a.sessions != nil && message.UserID != "" {
		if err := a.sessions.RecordInbound(ctx, message.UserID, message.Timestamp); err != nil {
			log.Printf("record inbound message: %v", err)
		}
	}

	// WhatsApp re-delivers a message (same ID) until it gets a 200, so dedup by
	// message ID before doing any work — otherwise a retry would write the entry
	// (and re-bill Gemini) a second time. Best-effort: on a store error we fall
	// through and process, favoring a possible duplicate over a dropped message.
	if a.sessions != nil && message.MessageID != "" {
		marked, derr := a.sessions.MarkProcessed(ctx, message.MessageID, message.Timestamp)
		if derr != nil {
			log.Printf("dedup check: %v", derr)
		} else if !marked {
			log.Printf("ignoring duplicate message_id=%s", message.MessageID)
			return Response{}, http.StatusOK, nil
		} else {
			// We claimed the marker before processing (so a near-simultaneous
			// retry is short-circuited). If the turn then ends without a 2xx —
			// a path WhatsApp will retry — drop the marker so that retry is
			// reprocessed instead of swallowed. Use a detached context so the
			// cleanup still runs when ctx is the reason we failed.
			defer func() {
				if err != nil || status < http.StatusOK || status >= http.StatusMultipleChoices {
					if uerr := a.sessions.Unmark(context.WithoutCancel(ctx), message.MessageID); uerr != nil {
						log.Printf("dedup unmark message_id=%s: %v", message.MessageID, uerr)
					}
				}
			}()
		}
	}

	// Every message — including one that starts with a slash — goes to the
	// agent. It reaches the ledger through the finance tools, which is also
	// where the sender's phone stops mattering: the tools are called with
	// shared.FinanceLedgerID (see orchestrator.agentGenerator), so the pharmacy
	// has one set of books no matter which of its two phones wrote to it.
	response, err := a.service.HandleMessage(ctx, message)
	if err != nil {
		return Response{}, http.StatusInternalServerError, err
	}

	if response.Text != "" {
		a.sendReply(ctx, req, response.Text)
	}

	return Response{Message: response.Text}, http.StatusOK, nil
}

func (a *App) sendReply(ctx context.Context, req Request, reply string) {
	if a.whatsappClient == nil || req.MessageID == "" {
		return
	}
	if err := a.whatsappClient.SendReply(ctx, req.PhoneNumberID, req.UserID, reply, req.MessageID); err != nil {
		log.Printf("send reply: %v", err)
	}
}

func (a *App) HandleVerification(mode, token, challenge string) events.APIGatewayV2HTTPResponse {
	if mode != "subscribe" {
		return jsonResponseOrDie(http.StatusBadRequest, map[string]string{"error": "invalid mode"})
	}
	if token != a.verifyToken {
		return jsonResponseOrDie(http.StatusForbidden, map[string]string{"error": "verify token mismatch"})
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       challenge,
	}
}

// jsonResponseOrDie is like jsonResponse but panics on error (never happens in practice).
func jsonResponseOrDie(statusCode int, payload any) events.APIGatewayV2HTTPResponse {
	resp, err := jsonResponse(statusCode, payload)
	if err != nil {
		panic(err)
	}
	return resp
}

func waTimestamp(ts string) string {
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ts
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// FromWAWebhook parses a Meta webhook envelope into one Request per inbound text
// message. A single POST can batch multiple entries/changes/messages, so all are
// iterated; status callbacks and non-text messages are skipped. An envelope with
// no text messages yields an empty slice (not an error); malformed JSON errors.
func FromWAWebhook(body []byte) ([]Request, error) {
	var wa waWebhook
	if err := json.Unmarshal(body, &wa); err != nil {
		return nil, err
	}

	var reqs []Request
	for _, entry := range wa.Entry {
		for _, change := range entry.Changes {
			val := change.Value
			for _, st := range val.Statuses {
				log.Printf("ignoring whatsapp status event status=%s message_id=%s", st.Status, st.ID)
			}
			for _, msg := range val.Messages {
				if msg.Type != "" && msg.Type != "text" {
					log.Printf("ignoring unsupported whatsapp message type=%s message_id=%s", msg.Type, msg.ID)
					continue
				}
				reqs = append(reqs, Request{
					UserID:        msg.From,
					MessageID:     msg.ID,
					PhoneNumberID: val.Metadata.PhoneNumberID,
					Text:          msg.Text.Body,
					Timestamp:     waTimestamp(msg.Timestamp),
				})
			}
		}
	}
	return reqs, nil
}

func normalize(req Request) (domain.Message, error) {
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

func validSignature(body []byte, signature, secret string) bool {
	received := strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(received), []byte(expected))
}

func decodeBase64Body(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}

func jsonResponse(statusCode int, payload any) (events.APIGatewayV2HTTPResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: statusCode,
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		Body: string(body),
	}, nil
}
