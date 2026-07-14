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
)

func TestHandleLambdaOK(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app := newTestApp(client)
	body := testTextWebhook()

	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body: body,
		Headers: map[string]string{
			"x-hub-signature-256": signString(body, app.secret),
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
	if client.markAsReadCalls != 1 {
		t.Fatalf("expected MarkAsRead to be called once, got %d", client.markAsReadCalls)
	}
	if client.sendReplyCalls != 1 {
		t.Fatalf("expected SendReply to be called once, got %d", client.sendReplyCalls)
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

	app := newTestApp(&fakeWhatsAppClient{})
	rawBody := testTextWebhook()

	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte(rawBody)),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"x-hub-signature-256": signString(rawBody, "test-secre"),
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

	app := newTestApp(&fakeWhatsAppClient{})
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

	app := newTestApp(&fakeWhatsAppClient{})
	rawBody := testTextWebhook()

	response, err := app.HandleLambda(context.Background(), events.APIGatewayV2HTTPRequest{
		Body:            base64.StdEncoding.EncodeToString([]byte(rawBody)),
		IsBase64Encoded: true,
		Headers: map[string]string{
			"x-hub-signature-256": signString(rawBody, app.secret),
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

func TestHandleWebhookHTTPAcceptsCanonicalHeaderCase(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app := newTestApp(client)
	body := []byte(testTextWebhook())

	response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodPost,
		Header: map[string]string{
			"X-Hub-Signature-256": signBytes(body, app.secret),
		},
		Body: body,
	})
	if err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if client.markAsReadCalls != 1 {
		t.Fatalf("expected MarkAsRead to be called once, got %d", client.markAsReadCalls)
	}
}

func TestHandleWebhookHTTPIgnoresStatusPayload(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app := newTestApp(client)
	body := []byte(testStatusWebhook())

	response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodPost,
		Header: map[string]string{
			"X-Hub-Signature-256": signBytes(body, app.secret),
		},
		Body: body,
	})
	if err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if response.Body != `{"ok":true}` {
		t.Fatalf("expected ok response, got %s", response.Body)
	}
	if client.markAsReadCalls != 0 {
		t.Fatalf("expected MarkAsRead not to be called, got %d", client.markAsReadCalls)
	}
	if client.sendReplyCalls != 0 {
		t.Fatalf("expected SendReply not to be called, got %d", client.sendReplyCalls)
	}
}

func TestHandleWebhookHTTPIgnoresUnsupportedMessageType(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app := newTestApp(client)
	body := []byte(testImageWebhook())

	response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodPost,
		Header: map[string]string{
			"X-Hub-Signature-256": signBytes(body, app.secret),
		},
		Body: body,
	})
	if err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if response.Body != `{"ok":true}` {
		t.Fatalf("expected ok response, got %s", response.Body)
	}
	if client.markAsReadCalls != 0 {
		t.Fatalf("expected MarkAsRead not to be called, got %d", client.markAsReadCalls)
	}
	if client.sendReplyCalls != 0 {
		t.Fatalf("expected SendReply not to be called, got %d", client.sendReplyCalls)
	}
}

func newTestApp(client *fakeWhatsAppClient) *App {
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
		nil,
		client,
		"test-secret",
		"test-verify-token",
	)
}

type fakeWhatsAppClient struct {
	markAsReadCalls int
	sendReplyCalls  int
}

func (f *fakeWhatsAppClient) MarkAsRead(context.Context, string, string) error {
	f.markAsReadCalls++
	return nil
}

func (f *fakeWhatsAppClient) SendReply(context.Context, string, string, string, string) error {
	f.sendReplyCalls++
	return nil
}

func signString(body, secret string) string {
	return signBytes([]byte(body), secret)
}

func signBytes(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func testTextWebhook() string {
	return `{
	  "object": "whatsapp_business_account",
	  "entry": [{
	    "id": "123456789",
	    "changes": [{
	      "field": "messages",
	      "value": {
	        "messaging_product": "whatsapp",
	        "metadata": {
	          "display_phone_number": "15550783881",
	          "phone_number_id": "123"
	        },
	        "contacts": [{
	          "profile": {"name": "User One"},
	          "wa_id": "u1"
	        }],
	        "messages": [{
	          "from": "u1",
	          "id": "wamid.HBgLMQ",
	          "timestamp": "1752465600",
	          "type": "text",
	          "text": {"body": "oi"}
	        }]
	      }
	    }]
	  }]
	}`
}

func testStatusWebhook() string {
	return `{
	  "object": "whatsapp_business_account",
	  "entry": [{
	    "id": "123456789",
	    "changes": [{
	      "field": "messages",
	      "value": {
	        "messaging_product": "whatsapp",
	        "metadata": {
	          "display_phone_number": "15550783881",
	          "phone_number_id": "123"
	        },
	        "statuses": [{
	          "id": "wamid.HBgLMQ",
	          "status": "read"
	        }]
	      }
	    }]
	  }]
	}`
}

func testImageWebhook() string {
	return `{
	  "object": "whatsapp_business_account",
	  "entry": [{
	    "id": "123456789",
	    "changes": [{
	      "field": "messages",
	      "value": {
	        "messaging_product": "whatsapp",
	        "metadata": {
	          "display_phone_number": "15550783881",
	          "phone_number_id": "123"
	        },
	        "contacts": [{
	          "profile": {"name": "User One"},
	          "wa_id": "u1"
	        }],
	        "messages": [{
	          "from": "u1",
	          "id": "wamid.HBgLMQ",
	          "timestamp": "1752465600",
	          "type": "image",
	          "image": {"mime_type": "image/jpeg", "sha256": "abc", "id": "media-1"}
	        }]
	      }
	    }]
	  }]
	}`
}
