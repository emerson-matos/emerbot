package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
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
	if response.Body != `{"ok":true}` {
		t.Fatalf("expected ok response body, got %s", response.Body)
	}
}

func TestHandleWebhookHTTPProcessesBatchedMessages(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app := newTestApp(client)
	body := []byte(testWebhookWithTexts("oi", "olá"))

	response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodPost,
		Header: map[string]string{"X-Hub-Signature-256": signBytes(body, app.secret)},
		Body:   body,
	})
	if err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if client.markAsReadCalls != 2 {
		t.Fatalf("expected MarkAsRead for both messages, got %d", client.markAsReadCalls)
	}
	if client.sendReplyCalls != 2 {
		t.Fatalf("expected SendReply for both messages, got %d", client.sendReplyCalls)
	}
}

func TestHandleWebhookHTTPHelpCommand(t *testing.T) {
	t.Parallel()

	for _, cmd := range []string{"/help", "/ajuda"} {
		client := &fakeWhatsAppClient{}
		app := newTestApp(client) // nil financial handler — /help must still work
		body := []byte(testWebhookWithTexts(cmd))

		response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
			Method: http.MethodPost,
			Header: map[string]string{"X-Hub-Signature-256": signBytes(body, app.secret)},
			Body:   body,
		})
		if err != nil {
			t.Fatalf("%s: HandleWebhookHTTP returned error: %v", cmd, err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: expected status 200, got %d", cmd, response.StatusCode)
		}
		if client.sendReplyCalls != 1 {
			t.Fatalf("%s: expected one help reply, got %d", cmd, client.sendReplyCalls)
		}
		if !strings.Contains(client.lastReply, "/despesa") || !strings.Contains(client.lastReply, "/resumo") {
			t.Fatalf("%s: expected reply to list commands, got %q", cmd, client.lastReply)
		}
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

func TestHandleWebhookHTTPVerification(t *testing.T) {
	t.Parallel()

	app := newTestApp(&fakeWhatsAppClient{})

	response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodGet,
		Query: map[string]string{
			"hub.mode":         "subscribe",
			"hub.verify_token": "test-verify-token",
			"hub.challenge":    "12345",
		},
	})
	if err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if response.Body != "12345" {
		t.Fatalf("expected challenge body, got %s", response.Body)
	}
}

func TestHandleWebhookHTTPRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	app := newTestApp(&fakeWhatsAppClient{})
	body := []byte(`{"object":`)

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
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
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
	lastReply       string
}

func (f *fakeWhatsAppClient) MarkAsRead(context.Context, string, string) error {
	f.markAsReadCalls++
	return nil
}

func (f *fakeWhatsAppClient) SendReply(_ context.Context, _, _, messageBody, _ string) error {
	f.sendReplyCalls++
	f.lastReply = messageBody
	return nil
}

// testWebhookWithTexts builds a Meta envelope carrying one text message per
// argument (all in a single entry/change) — used for batching and /help tests.
func testWebhookWithTexts(texts ...string) string {
	msgs := make([]string, len(texts))
	for i, txt := range texts {
		msgs[i] = fmt.Sprintf(`{"from":"u1","id":"wamid.%d","timestamp":"1752465600","type":"text","text":{"body":%q}}`, i, txt)
	}
	return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"id":"1","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"123"},"messages":[%s]}}]}]}`, strings.Join(msgs, ","))
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
