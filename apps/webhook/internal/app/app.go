package app

import (
	"crypto/subtle"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
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

type App struct {
	service *orchestrator.Service
	secret  string
}

func New(service *orchestrator.Service, secret string) *App {
	return &App{
		service: service,
		secret:  secret,
	}
}

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

	response, err := a.service.HandleMessage(ctx, message)
	if err != nil {
		return Response{}, http.StatusInternalServerError, err
	}

	return Response{Message: response.Text}, http.StatusOK, nil
}

func (a *App) HandleLambda(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if event.RequestContext.HTTP.Method != http.MethodPost {
		return jsonResponse(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}

	var req Request
	if err := json.Unmarshal([]byte(event.Body), &req); err != nil {
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
