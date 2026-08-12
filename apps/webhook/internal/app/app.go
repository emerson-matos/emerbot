package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/wainbound"
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

// This handler does four things: check Meta's signature, extract the messages,
// put each one on the queue, and answer 200. It does not know what Gemini is,
// has no conversation history, no tools and no access to the business DynamoDB
// — all of that moved to apps/worker (ADR-028), because a question that calls a
// tool takes tens of seconds and Meta was waiting for it.
//
// There is deliberately no slash-command layer here either. Every message goes
// to the agent, which writes the ledger through the finance tools
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
//
// Dedup is deliberately absent: "já processamos esta mensagem" is a question
// about a turn, and the turn happens in the worker, which is where it is now
// asked and answered (ADR-029).
type SessionStore interface {
	RecordInbound(ctx context.Context, phone string, at time.Time) error
}

// Publisher hands an extracted message to the worker. This is the webhook's
// whole contract with the rest of the system: recebi e enfileirei.
type Publisher interface {
	Publish(ctx context.Context, env wainbound.Envelope) error
}

type App struct {
	queue          Publisher
	whatsappClient whatsapp.Client
	sessions       SessionStore
	secret         string
	verifyToken    string
}

func New(queue Publisher, waClient whatsapp.Client, secret, verifyToken string, sessions SessionStore) *App {
	if verifyToken == "" {
		verifyToken = secret
	}
	return &App{
		queue:          queue,
		whatsappClient: waClient,
		sessions:       sessions,
		secret:         secret,
		verifyToken:    verifyToken,
	}
}

func NewFromEnv(secret, graphAPIToken string) *App {
	var queue Publisher
	if queueURL := shared.Getenv("WHATSAPP_INBOUND_QUEUE_URL", ""); queueURL != "" {
		publisher, err := wainbound.NewSQSPublisher(context.Background(), queueURL, shared.Getenv("SQS_ENDPOINT", ""))
		if err != nil {
			log.Fatalf("NewFromEnv: inbound queue: %v", err)
		}
		queue = publisher
	}
	return NewFromEnvWithPublisher(secret, graphAPIToken, queue)
}

// NewFromEnvWithPublisher is NewFromEnv with the queue chosen by the caller. It
// exists for the local entrypoint, which has no queue at all and runs the turn
// in the same process (see apps/webhook/cmd/local).
func NewFromEnvWithPublisher(secret, graphAPIToken string, queue Publisher) *App {
	var sessions SessionStore
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")

	// The 24h-window session store lives in its own table (TTL-managed).
	if sessTable := shared.Getenv("WHATSAPP_SESSIONS_TABLE", ""); sessTable != "" {
		ctx := context.Background()
		sessStore, err := wasession.NewDynamoDBStore(ctx, sessTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: session store: %v", err)
		}
		sessions = sessStore
	}

	waClient := whatsapp.NewClientFromEnv(graphAPIToken)
	verifyToken := shared.Getenv("WEBHOOK_VERIFY_TOKEN", secret)

	return New(queue, waClient, secret, verifyToken, sessions)
}

// Handle takes one extracted message as far as the queue and reports the status
// Meta should get for it.
func (a *App) Handle(ctx context.Context, req Request) (int, error) {
	message, err := normalize(req)
	if err != nil {
		return http.StatusBadRequest, err
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

	// Enqueueing is the only thing whose failure changes the answer: the two
	// calls above are true whether or not the turn ever runs (the window was
	// opened by the person messaging us; the message was read), while a message
	// that did not reach the queue is a message nobody will ever answer. So it
	// is a 500 and Meta redelivers.
	//
	// A redelivery is not a second turn: the same Meta message id is the FIFO
	// MessageDeduplicationId, and behind it the worker's mark in DynamoDB
	// answers the same question without a time window (ADR-029).
	if a.queue == nil {
		return http.StatusInternalServerError, errors.New("no inbound queue configured")
	}
	env := wainbound.Envelope{Message: message, PhoneNumberID: req.PhoneNumberID}
	if err := a.queue.Publish(ctx, env); err != nil {
		return http.StatusInternalServerError, fmt.Errorf("enqueue message %s: %w", message.MessageID, err)
	}

	return http.StatusOK, nil
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
