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
	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/wasession"
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

// A turn that fails must not end in silence: a message that gets no reply is
// indistinguishable from one that never arrived. Having told the user is also
// what makes the 200 correct — a WhatsApp retry would re-run the same failing
// turn and, when it worked, answer the same question twice.
func TestFailedTurnTellsTheUserInsteadOfGoingQuiet(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app := newFailingApp(client)
	body := []byte(testWebhookWithTexts("como estamos"))

	response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodPost,
		Header: map[string]string{"X-Hub-Signature-256": signBytes(body, app.secret)},
		Body:   body,
	})
	if err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("told the user, so the delivery is handled: expected 200, got %d", response.StatusCode)
	}
	if client.sendReplyCalls != 1 {
		t.Fatalf("expected exactly one fallback message, got %d sends", client.sendReplyCalls)
	}
	if client.lastReply != fallbackReply {
		t.Fatalf("reply = %q, want the fallback", client.lastReply)
	}
}

// The retry only earns its keep when the user could not be reached at all —
// then silence is what they got, and WhatsApp trying again is the second chance.
func TestFailedTurnHandsBackToWhatsAppWhenTheUserCannotBeReached(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{sendReplyErr: fmt.Errorf("graph api unreachable")}
	app := newFailingApp(client)
	body := []byte(testWebhookWithTexts("como estamos"))

	response, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodPost,
		Header: map[string]string{"X-Hub-Signature-256": signBytes(body, app.secret)},
		Body:   body,
	})
	if err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("nobody was told, so Meta must retry: expected 500, got %d", response.StatusCode)
	}
}

func newTestApp(client *fakeWhatsAppClient) *App {
	stores := orchestrator.NewInMemoryStores()
	if err := stores.Save(context.Background(), domain.Memory{
		UserID: "u1",
		Type:   "Goal",
		ID:     "LearnAWS",
		Value:  "Study Lambda architecture locally first.",
	}); err != nil {
		panic(err)
	}

	return New(
		orchestrator.NewService(orchestrator.Config{}),
		client,
		"test-secret",
		"test-verify-token",
		nil,
	)
}

type failingLLM struct{}

func (failingLLM) Generate(context.Context, orchestrator.Input) (orchestrator.Output, error) {
	return orchestrator.Output{}, fmt.Errorf("llm unavailable")
}

func newFailingApp(client *fakeWhatsAppClient) *App {
	return New(
		orchestrator.NewServiceWithGenerator(failingLLM{}),
		client,
		"test-secret",
		"test-verify-token",
		nil,
	)
}

// TestHandleReprocessesAfterUnreachableTurn proves the dedup marker is
// compensated on the one path that still needs it: the user could not be told,
// so the turn ends without a 2xx, the marker is dropped, and WhatsApp's retry
// of the same message ID is reprocessed rather than swallowed as a duplicate.
//
// In production this failed the other way round — the Unmark had no
// dynamodb:DeleteItem, so the marker survived and every retry was answered
// "ignoring duplicate": a message that failed once could never succeed.
func TestHandleReprocessesAfterUnreachableTurn(t *testing.T) {
	t.Parallel()

	sessions := wasession.NewInMemoryStore()
	client := &fakeWhatsAppClient{sendReplyErr: fmt.Errorf("graph api unreachable")}
	app := New(orchestrator.NewServiceWithGenerator(failingLLM{}), client, "secret", "verify", sessions)

	req := Request{UserID: "u1", MessageID: "wamid.FAIL", Text: "olá"}

	if _, status, _ := app.Handle(context.Background(), req); status != http.StatusInternalServerError {
		t.Fatalf("first delivery: expected 500, got %d", status)
	}
	if _, status, _ := app.Handle(context.Background(), req); status != http.StatusInternalServerError {
		t.Fatalf("retry after an unreachable turn must reprocess (500), got %d", status)
	}
}

// The mirror image: once the user has been told, the retry is the wrong
// outcome. The marker stays, so WhatsApp's second delivery is swallowed and
// nobody is answered twice for one question.
func TestHandleDoesNotReprocessOnceTheUserWasTold(t *testing.T) {
	t.Parallel()

	sessions := wasession.NewInMemoryStore()
	client := &fakeWhatsAppClient{}
	app := New(orchestrator.NewServiceWithGenerator(failingLLM{}), client, "secret", "verify", sessions)

	req := Request{UserID: "u1", MessageID: "wamid.TOLD", Text: "como estamos"}

	if _, status, _ := app.Handle(context.Background(), req); status != http.StatusOK {
		t.Fatalf("first delivery: expected 200, got %d", status)
	}
	if _, status, _ := app.Handle(context.Background(), req); status != http.StatusOK {
		t.Fatalf("retry: expected 200, got %d", status)
	}
	if client.sendReplyCalls != 1 {
		t.Fatalf("expected the fallback exactly once across both deliveries, got %d", client.sendReplyCalls)
	}
}

type fakeWhatsAppClient struct {
	markAsReadCalls int
	sendReplyCalls  int
	sendTextCalls   int
	lastReply       string
	lastText        string
	// sendReplyErr makes SendReply fail, which is the case that decides
	// whether a failed turn is ours to own or WhatsApp's to retry.
	sendReplyErr error
}

func (f *fakeWhatsAppClient) MarkAsRead(context.Context, string, string) error {
	f.markAsReadCalls++
	return nil
}

func (f *fakeWhatsAppClient) SendReply(_ context.Context, _, _, messageBody, _ string) error {
	f.sendReplyCalls++
	f.lastReply = messageBody
	return f.sendReplyErr
}

func (f *fakeWhatsAppClient) SendText(_ context.Context, _, _, messageBody string) error {
	f.sendTextCalls++
	f.lastText = messageBody
	return nil
}

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
