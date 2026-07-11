package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/tools"
)

func TestHandleLambdaOK(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body: `{"user_id":"u1","message_id":"m1","text":"oi","timestamp":"2026-07-11T00:00:00Z","signature":"test-secret"}`,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodPost,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleLambda returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	var payload Response
	if err := json.Unmarshal([]byte(response.Body), &payload); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if payload.Message == "" {
		t.Fatal("expected non-empty response message")
	}
}

func TestHandleLambdaRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body: `{"user_id":"u1","message_id":"m1","text":"oi","signature":"wrong"}`,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodPost,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleLambda returned error: %v", err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
}

func TestHandleLambdaRejectsInvalidMethod(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method: http.MethodGet,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleLambda returned error: %v", err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", response.StatusCode)
	}
}

func newTestApp() *App {
	stores := memory.NewInMemoryStores()
	_ = stores.Save(context.Background(), domain.Memory{
		UserID: "u1",
		Type:   "Goal",
		ID:     "LearnAWS",
		Value:  "Study Lambda architecture locally first.",
	})

	return New(
		orchestrator.NewService(
			llm.StaticClient{},
			stores,
			stores,
			tools.NewRegistry(tools.EchoTool{}),
		),
		"test-secret",
	)
}
