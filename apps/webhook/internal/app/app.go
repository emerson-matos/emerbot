package app

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/emerson/emerbot/apps/webhook/internal/financial"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/tools"
)

type Request struct {
	UserID    string `json:"user_id"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Signature string `json:"signature"`
}

type Response struct {
	Message string `json:"message"`
}

// financialCommands are prefixes that route to the financial handler instead
// of the AI orchestrator.
var financialCommands = []string{"/despesa", "/receita", "/pagar", "/receber", "/resumo"}

type App struct {
	service           *orchestrator.Service
	financialHandler  *financial.Handler
	secret            string
}

func New(service *orchestrator.Service, finHandler *financial.Handler, secret string) *App {
	return &App{
		service:          service,
		financialHandler: finHandler,
		secret:           secret,
	}
}

// NewDefault builds an App with in-memory stores and a static LLM client.
// Used for local development without Docker. The financial handler uses a
// nil store (no-op) unless DYNAMODB_ENDPOINT is set — see cmd/local for wiring.
func NewDefault(secret string) *App {
	stores := memory.NewInMemoryStores()
	if err := stores.Save(context.Background(), domain.Memory{
		UserID: "demo-user",
		Type:   "Preference",
		ID:     "Language",
		Value:  "pt-BR",
	}); err != nil {
		log.Printf("failed to seed default memory: %v", err)
	}

	return New(
		orchestrator.NewService(
			llm.StaticClient{},
			stores,
			stores,
			tools.NewRegistry(tools.EchoTool{}),
		),
		nil, // financial handler not wired in NewDefault; use New() directly
		secret,
	)
}

func (a *App) Handle(ctx context.Context, req Request) (Response, int, error) {
	if !validSignature(req.Signature, a.secret) {
		return Response{}, http.StatusUnauthorized, fmt.Errorf("invalid signature")
	}

	message, err := normalize(req)
	if err != nil {
		return Response{}, http.StatusBadRequest, err
	}

	// Route financial commands to the financial handler.
	text := strings.TrimSpace(message.Text)
	if a.financialHandler != nil && isFinancialCommand(text) {
		var reply string
		var err error
		if strings.HasPrefix(strings.ToLower(text), "/resumo") {
			reply, err = a.financialHandler.Resumo(ctx, message.UserID)
		} else {
			reply, err = a.financialHandler.Handle(ctx, message.UserID, text)
		}
		if err != nil {
			log.Printf("financial handler error: %v", err)
		}
		return Response{Message: reply}, http.StatusOK, nil
	}

	response, err := a.service.HandleMessage(ctx, message)
	if err != nil {
		return Response{}, http.StatusInternalServerError, err
	}

	return Response{Message: response.Text}, http.StatusOK, nil
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

func (a *App) HandleLambda(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if event.RequestContext.HTTP.Method != http.MethodPost {
		return jsonResponse(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}

	body := []byte(event.Body)
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(event.Body)
		if err != nil {
			return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid base64 body"})
		}
		body = decoded
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]string{"error": "invalid json"})
	}

	resp, status, err := a.Handle(ctx, req)
	if err != nil {
		return jsonResponse(status, map[string]string{"error": err.Error()})
	}

	return jsonResponse(status, resp)
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

func validSignature(signature, secret string) bool {
	signature = strings.TrimSpace(signature)
	secret = strings.TrimSpace(secret)
	if signature == "" || secret == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(signature), []byte(secret)) == 1
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
