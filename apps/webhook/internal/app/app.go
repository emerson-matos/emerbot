package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/emerson/emerbot/apps/webhook/internal/financial"
	"github.com/emerson/emerbot/packages/domain"
	pkgfinance "github.com/emerson/emerbot/packages/finance"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/shared"
	"github.com/emerson/emerbot/packages/tools"
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

// financialCommands are prefixes that route to the financial handler instead
// of the AI orchestrator.
var financialCommands = []string{"/despesa", "/receita", "/pagar", "/receber", "/recorrente", "/resumo", "/goal", "/meta"}

// commandHelp is the user-facing catalog shown by /help. Kept next to
// financialCommands so the two stay in sync.
var commandHelp = []struct{ usage, desc string }{
	{"/despesa <valor> <categoria> [data] [descrição]", "registra uma despesa já paga"},
	{"/receita <valor> <categoria> [data] [descrição]", "registra uma receita já recebida"},
	{"/pagar <valor> <categoria> [data] [descrição]", "agenda uma despesa a pagar"},
	{"/receber <valor> <categoria> [data] [descrição]", "agenda uma receita a receber"},
	{"/recorrente <pagar|receber> <valor> <categoria> <periodo> <n> [data] [descrição]", "cria uma série de N lançamentos pendentes (ex: aluguel mensal por 12 meses)"},
	{"/resumo", "resumo financeiro do mês"},
	{"/goal", "progresso das metas do mês"},
	{"/meta <faturamento> <despesa>", "define as metas do mês"},
	{"/help", "mostra esta ajuda"},
}

func helpText() string {
	var b strings.Builder
	b.WriteString("🤖 *Comandos disponíveis:*\n\n")
	for _, c := range commandHelp {
		fmt.Fprintf(&b, "*%s*\n%s\n\n", c.usage, c.desc)
	}
	b.WriteString("Ex: /despesa 500 aluguel Aluguel da loja\n\n")
	b.WriteString("💡 Envie um comando sozinho (ex: /despesa) para ver o passo a passo.")
	return b.String()
}

// firstToken returns the text up to the first space (the command word).
func firstToken(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

type App struct {
	service          *orchestrator.Service
	financialHandler *financial.Handler
	whatsappClient   whatsapp.Client
	secret           string
	verifyToken      string
}

func New(service *orchestrator.Service, finHandler *financial.Handler, waClient whatsapp.Client, secret, verifyToken string) *App {
	if verifyToken == "" {
		verifyToken = secret
	}
	return &App{
		service:          service,
		financialHandler: finHandler,
		whatsappClient:   waClient,
		secret:           secret,
		verifyToken:      verifyToken,
	}
}

func NewFromEnv(secret, graphAPIToken string) *App {
	var finHandler *financial.Handler
	finTable := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "")
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")
	if finTable != "" {
		ctx := context.Background()
		store, err := pkgfinance.NewDynamoDBStore(ctx, finTable, endpoint)
		if err != nil {
			log.Fatalf("NewFromEnv: finance store: %v", err)
		}
		parser := whatsapp.NewRegexParser()
		finHandler = financial.NewHandler(parser, store)
	}

	stores := memory.NewInMemoryStores()
	if err := stores.Save(context.Background(), domain.Memory{
		UserID: "demo-user",
		Type:   "Preference",
		ID:     "Language",
		Value:  "pt-BR",
	}); err != nil {
		log.Printf("NewFromEnv: seed memory: %v", err)
	}

	svc := orchestrator.NewService(
		llm.StaticClient{},
		stores, stores,
		tools.NewRegistry(tools.EchoTool{}),
	)

	waClient := whatsapp.NewClientFromEnv(graphAPIToken)
	verifyToken := shared.Getenv("WEBHOOK_VERIFY_TOKEN", secret)

	return New(svc, finHandler, waClient, secret, verifyToken)
}

func (a *App) Handle(ctx context.Context, req Request) (Response, int, error) {
	message, err := normalize(req)
	if err != nil {
		return Response{}, http.StatusBadRequest, err
	}

	if a.whatsappClient != nil {
		if err := a.whatsappClient.MarkAsRead(ctx, req.PhoneNumberID, req.MessageID); err != nil {
			log.Printf("mark as read: %v", err)
		}
	}

	text := strings.TrimSpace(message.Text)

	// /help (and pt-BR alias /ajuda) — handled before financial routing so it
	// works even when no financial handler is wired.
	if cmd := firstToken(text); strings.EqualFold(cmd, "/help") || strings.EqualFold(cmd, "/ajuda") {
		reply := helpText()
		a.sendReply(ctx, req, reply)
		return Response{Message: reply}, http.StatusOK, nil
	}

	// Route financial commands to the financial handler.
	if a.financialHandler != nil && isFinancialCommand(text) {
		var reply string
		var err error
		// TODO(mock): all senders write/read one shared finance ledger until
		// phone→account linking exists. Replies still route to req.UserID.
		ledgerID := shared.FinanceLedgerID
		if strings.HasPrefix(strings.ToLower(text), "/resumo") {
			reply, err = a.financialHandler.Resumo(ctx, ledgerID)
		} else if strings.HasPrefix(strings.ToLower(text), "/goal") {
			reply, err = a.financialHandler.Goal(ctx, ledgerID)
		} else if strings.HasPrefix(strings.ToLower(text), "/meta") {
			reply, err = a.financialHandler.SetGoal(ctx, ledgerID, text)
		} else if strings.HasPrefix(strings.ToLower(text), "/recorrente") {
			reply, err = a.financialHandler.Recorrente(ctx, ledgerID, text)
		} else {
			reply, err = a.financialHandler.Handle(ctx, ledgerID, text)
		}
		if err != nil {
			log.Printf("financial handler error: %v", err)
		}
		if reply != "" {
			a.sendReply(ctx, req, reply)
		}
		return Response{Message: reply}, http.StatusOK, nil
	}

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

func isFinancialCommand(text string) bool {
	lower := strings.ToLower(text)
	for _, cmd := range financialCommands {
		if strings.HasPrefix(lower, cmd) {
			return true
		}
	}
	return false
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
