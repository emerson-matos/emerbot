package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/emerson/emerbot/packages/wainbound"
)

func TestHandleLambdaOK(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app, queue := newTestApp(client)
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
	// The webhook does not answer anybody: it hands the message over and says
	// so. Whatever the reply turns out to be is the worker's business (ADR-028).
	if client.sendReplyCalls != 0 {
		t.Fatalf("the webhook must not reply, got %d sends", client.sendReplyCalls)
	}
	if len(queue.published) != 1 {
		t.Fatalf("published %d messages, want 1", len(queue.published))
	}
	if response.Body != `{"ok":true}` {
		t.Fatalf("expected ok response body, got %s", response.Body)
	}
}

func TestHandleWebhookHTTPProcessesBatchedMessages(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app, queue := newTestApp(client)
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
	if len(queue.published) != 2 {
		t.Fatalf("published %d messages, want both of them", len(queue.published))
	}
}

func TestHandleLambdaRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(&fakeWhatsAppClient{})
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

	app, _ := newTestApp(&fakeWhatsAppClient{})
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

	app, _ := newTestApp(&fakeWhatsAppClient{})
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

	app, _ := newTestApp(&fakeWhatsAppClient{})

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

	app, _ := newTestApp(&fakeWhatsAppClient{})
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
	app, _ := newTestApp(client)
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
	app, queue := newTestApp(client)
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
	if len(queue.published) != 0 {
		t.Fatalf("published %d messages, want nothing enqueued", len(queue.published))
	}
}

func TestHandleWebhookHTTPIgnoresUnsupportedMessageType(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app, queue := newTestApp(client)
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
	if len(queue.published) != 0 {
		t.Fatalf("published %d messages, want nothing enqueued", len(queue.published))
	}
}

// The webhook's answer to Meta now depends on one thing only: did the message
// reach the queue. A failure there is transient by nature — nobody will ever
// answer a message that was never enqueued — so Meta is asked to redeliver.
func TestAFailedEnqueueIsHandedBackToMeta(t *testing.T) {
	t.Parallel()

	client := &fakeWhatsAppClient{}
	app, queue := newTestApp(client)
	queue.err = errors.New("sqs is down")
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
		t.Fatalf("nothing was enqueued, so Meta must retry: expected 500, got %d", response.StatusCode)
	}
}

// A webhook with nowhere to put the message is broken, not idle: saying 200
// would tell Meta the message is handled and end its retries, which is the one
// way to lose a message for good.
func TestAWebhookWithoutAQueueRefusesTheMessage(t *testing.T) {
	t.Parallel()

	app := New(nil, &fakeWhatsAppClient{}, "secret", "verify", nil)

	status, err := app.Handle(context.Background(), Request{UserID: "u1", MessageID: "m1", Text: "oi"})
	if err == nil {
		t.Fatal("expected an error when there is no queue configured")
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
}

// What goes on the queue is the message this repo already normalized, plus the
// business number the worker has to answer from — never Meta's envelope
// (ADR-028).
func TestTheEnqueuedEnvelopeCarriesWhatTheWorkerNeeds(t *testing.T) {
	t.Parallel()

	app, queue := newTestApp(&fakeWhatsAppClient{})
	body := []byte(testTextWebhook())

	if _, err := app.HandleWebhookHTTP(context.Background(), WebhookHTTPRequest{
		Method: http.MethodPost,
		Header: map[string]string{"X-Hub-Signature-256": signBytes(body, app.secret)},
		Body:   body,
	}); err != nil {
		t.Fatalf("HandleWebhookHTTP returned error: %v", err)
	}
	if len(queue.published) != 1 {
		t.Fatalf("published %d messages, want 1", len(queue.published))
	}
	env := queue.published[0]
	if env.Message.UserID != "u1" || env.Message.Text != "oi" || env.Message.MessageID != "wamid.HBgLMQ" {
		t.Fatalf("message = %+v, want the extracted fields", env.Message)
	}
	if env.PhoneNumberID != "123" {
		t.Fatalf("phone number id = %q, want the one from Meta's metadata", env.PhoneNumberID)
	}
	if want := time.Date(2025, 7, 14, 4, 0, 0, 0, time.UTC); !env.Message.Timestamp.Equal(want) {
		t.Fatalf("timestamp = %v, want %v", env.Message.Timestamp, want)
	}
}

func newTestApp(client *fakeWhatsAppClient) (*App, *fakePublisher) {
	queue := &fakePublisher{}
	return New(queue, client, "test-secret", "test-verify-token", nil), queue
}

// fakePublisher stands in for the queue: the webhook's whole output is what it
// hands over and whether the handover worked.
type fakePublisher struct {
	published []wainbound.Envelope
	err       error
}

func (f *fakePublisher) Publish(_ context.Context, env wainbound.Envelope) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, env)
	return nil
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
