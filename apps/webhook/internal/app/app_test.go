package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/llm"
	"github.com/emerson/emerbot/packages/memory"
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/tools"
	"github.com/emerson/emerbot/packages/whatsapp"
)

func TestHandleLambdaOK(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	body := testWebhook()

	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body: body,
		Headers: map[string]string{
			"x-hub-signature-256": sign(body, app.secret),
		},
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
	rawBody := testWebhook()

	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte(rawBody)),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"x-hub-signature-256": sign(rawBody, "test-secre"),
		},
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
				Method: http.MethodPut,
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

func TestHandleLambdaAcceptsBase64EncodedBody(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	rawBody := testWebhook()

	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte(rawBody)),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"x-hub-signature-256": sign(rawBody, app.secret),
		},
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
}

func newTestApp() *App {
	stores := memory.NewInMemoryStores()
	if err := stores.Save(context.Background(), domain.Memory{
		UserID: "u1",
		Type:   "Goal",
		ID:     "LearnAWS",
		Value:  "Study Lambda architecture locally first.",
	}); err != nil {
		panic(err)
	}

	return New(
		orchestrator.NewService(
			llm.StaticClient{},
			stores,
			stores,
			tools.NewRegistry(tools.EchoTool{}),
		),
		nil,                         // no financial handler in tests
		whatsapp.NewLocalClient(""), // no whatsapp client in tests
		"test-secret",
		"test-verify-token",
	)
}

func sign(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func testWebhook() string {
	return `{
      "object":"whatsapp_business_account",
      "entry":[{
        "changes":[{
          "value":{
            "metadata":{
              "phone_number_id":"123"
            },
            "messages":[{
              "from":"u1",
              "id":"m1",
              "timestamp":"1752465600",
              "text":{"body":"oi"}
            }]
          }
        }]
      }]
    }`
}
