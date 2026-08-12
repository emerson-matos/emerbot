package app

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/orchestrator"
	"github.com/emerson/emerbot/packages/wasession"
)

// Coverage for the webhook's plumbing: verification handshake, reply
// dispatch, and the small parsing/response helpers everything else routes
// through.

func TestNewFallsBackToTheSecretAsVerifyToken(t *testing.T) {
	// Meta's handshake token defaults to the app secret when unset, so a
	// deployment that only configures one value still verifies.
	app := New(nil, nil, "shhh", "", nil)
	if app.verifyToken != "shhh" {
		t.Fatalf("verifyToken = %q, want it defaulted to the secret", app.verifyToken)
	}

	explicit := New(nil, nil, "shhh", "outro", nil)
	if explicit.verifyToken != "outro" {
		t.Fatalf("verifyToken = %q, want the explicit token to win", explicit.verifyToken)
	}
}

func TestNewFromEnvWithNoTablesConfigured(t *testing.T) {
	// With no table names set, nothing touches AWS: the orchestrator falls back
	// to its in-memory stores and the WhatsApp client to the simulator. This is
	// the path `make run-webhook` takes.
	t.Setenv("FINANCIAL_ENTRIES_TABLE", "")
	t.Setenv("CONVERSATIONS_TABLE", "")
	t.Setenv("WHATSAPP_SESSIONS_TABLE", "")
	t.Setenv("WEBHOOK_VERIFY_TOKEN", "")

	app := NewFromEnv("secret-do-app", "")
	if app == nil {
		t.Fatal("NewFromEnv returned nil")
	}
	if app.service == nil {
		t.Fatal("the orchestrator service must always be wired")
	}
	if app.sessions != nil {
		t.Fatal("no sessions table means no session store")
	}
	// WEBHOOK_VERIFY_TOKEN unset falls back to the app secret.
	if app.verifyToken != "secret-do-app" {
		t.Fatalf("verifyToken = %q, want the secret as fallback", app.verifyToken)
	}
	if app.whatsappClient == nil {
		t.Fatal("a WhatsApp client must be selected even without a Graph token")
	}
}

func TestNewFromEnvHonoursTheVerifyToken(t *testing.T) {
	t.Setenv("FINANCIAL_ENTRIES_TABLE", "")
	t.Setenv("CONVERSATIONS_TABLE", "")
	t.Setenv("WHATSAPP_SESSIONS_TABLE", "")
	t.Setenv("WEBHOOK_VERIFY_TOKEN", "token-da-meta")

	if app := NewFromEnv("secret-do-app", ""); app.verifyToken != "token-da-meta" {
		t.Fatalf("verifyToken = %q, want the configured token", app.verifyToken)
	}
}

func TestHandleVerification(t *testing.T) {
	app := New(nil, nil, "secret", "verify-me", nil)

	t.Run("a correct handshake echoes the challenge", func(t *testing.T) {
		resp := app.HandleVerification("subscribe", "verify-me", "challenge-123")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		// Meta requires the raw challenge back, not JSON.
		if resp.Body != "challenge-123" {
			t.Fatalf("body = %q, want the challenge echoed verbatim", resp.Body)
		}
		if resp.Headers["Content-Type"] != "text/plain" {
			t.Fatalf("Content-Type = %q, want text/plain", resp.Headers["Content-Type"])
		}
	})

	t.Run("a wrong mode is rejected", func(t *testing.T) {
		resp := app.HandleVerification("unsubscribe", "verify-me", "challenge-123")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("a wrong token is rejected", func(t *testing.T) {
		resp := app.HandleVerification("subscribe", "palpite", "challenge-123")
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		// The challenge must not leak to a caller that failed the token check.
		if resp.Body == "challenge-123" {
			t.Fatal("the challenge was echoed despite a failed token check")
		}
	})
}

func TestSendReplyIsSkippedWhenThereIsNothingToReplyTo(t *testing.T) {
	client := &fakeWhatsAppClient{}
	app := New(nil, client, "secret", "verify", nil)

	// No message id: there is no inbound message to attach a reply to.
	if err := app.sendReply(context.Background(), Request{UserID: "u1"}, "olá"); err != nil {
		t.Fatalf("nothing to reply to is not an error: %v", err)
	}
	if client.sendReplyCalls != 0 {
		t.Fatalf("sendReply issued %d calls, want none without a message id", client.sendReplyCalls)
	}

	// No client configured at all must not panic.
	if err := New(nil, nil, "secret", "verify", nil).
		sendReply(context.Background(), Request{UserID: "u1", MessageID: "m1"}, "olá"); err != nil {
		t.Fatalf("no client configured is not an error: %v", err)
	}
}

// sendReply now reports a transport failure instead of swallowing it, because
// the failure path needs to know: having reached the user is what decides
// between owning the turn and handing it back to WhatsApp.
func TestSendReplyReportsATransportError(t *testing.T) {
	client := &erroringWhatsAppClient{}
	app := New(nil, client, "secret", "verify", nil)

	if err := app.sendReply(context.Background(), Request{UserID: "u1", MessageID: "m1"}, "olá"); err == nil {
		t.Fatal("a failed send must be reported to the caller")
	}
	if client.calls != 1 {
		t.Fatalf("sendReply issued %d calls, want 1", client.calls)
	}
}

// The invariant that moved rather than disappeared: on a turn that *succeeded*,
// a failed send is still logged and not propagated. The work is already done —
// making WhatsApp retry would run it a second time.
func TestSuccessfulTurnStaysHandledWhenTheReplyFailsToSend(t *testing.T) {
	client := &erroringWhatsAppClient{}
	app := New(orchestrator.NewService(orchestrator.Config{}), client, "secret", "verify", nil)

	_, status, err := app.Handle(context.Background(), Request{UserID: "u1", MessageID: "m1", Text: "oi"})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the turn succeeded, only the send failed", status)
	}
}

func TestWaTimestamp(t *testing.T) {
	// Meta sends epoch seconds; anything else is passed through untouched so a
	// format change degrades rather than crashing.
	cases := map[string]string{
		"1753286400": "2025-07-23T16:00:00Z",
		"0":          "1970-01-01T00:00:00Z",
		"":           "",
		"not-a-time": "not-a-time",
	}
	for in, want := range cases {
		if got := waTimestamp(in); got != want {
			t.Fatalf("waTimestamp(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	t.Run("trims every field", func(t *testing.T) {
		got, err := normalize(Request{
			UserID: "  u1 ", Text: "  oi  ", MessageID: " m1 ",
			Timestamp: "2026-07-20T10:00:00Z",
		})
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if got.UserID != "u1" || got.Text != "oi" || got.MessageID != "m1" {
			t.Fatalf("message = %+v, want the fields trimmed", got)
		}
		if !got.Timestamp.Equal(time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)) {
			t.Fatalf("timestamp = %v, want the parsed value", got.Timestamp)
		}
	})

	t.Run("an absent timestamp defaults to now", func(t *testing.T) {
		before := time.Now().UTC()
		got, err := normalize(Request{UserID: "u1", Text: "oi", MessageID: "m1", Timestamp: "   "})
		if err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if got.Timestamp.Before(before) {
			t.Fatalf("timestamp = %v, want it defaulted to roughly now", got.Timestamp)
		}
	})

	t.Run("a malformed timestamp is an error", func(t *testing.T) {
		// Guessing here would silently file a message under the wrong day.
		if _, err := normalize(Request{UserID: "u1", Timestamp: "20/07/2026"}); err == nil {
			t.Fatal("expected an error for an unparseable timestamp")
		}
	})
}

func TestHeaderValueIsCaseInsensitive(t *testing.T) {
	headers := map[string]string{"X-Hub-Signature-256": "sha256=abc"}

	// API Gateway lowercases header names while a local server keeps the
	// canonical case, so lookups must not depend on either.
	for _, key := range []string{"X-Hub-Signature-256", "x-hub-signature-256", "X-HUB-SIGNATURE-256"} {
		if got := headerValue(headers, key); got != "sha256=abc" {
			t.Fatalf("headerValue(%q) = %q, want the signature", key, got)
		}
	}
	if got := headerValue(headers, "X-Missing"); got != "" {
		t.Fatalf("headerValue for an absent key = %q, want empty", got)
	}
	if got := headerValue(nil, "anything"); got != "" {
		t.Fatalf("headerValue on nil headers = %q, want empty", got)
	}
}

func TestJSONResponseHelpers(t *testing.T) {
	resp, err := jsonResponse(http.StatusTeapot, map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("jsonResponse: %v", err)
	}
	if resp.StatusCode != http.StatusTeapot || resp.Body != `{"a":"b"}` {
		t.Fatalf("response = %+v, want the status and marshalled body", resp)
	}
	if resp.Headers["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", resp.Headers["Content-Type"])
	}

	orDie := jsonResponseOrDie(http.StatusBadRequest, map[string]string{"error": "nope"})
	if orDie.StatusCode != http.StatusBadRequest || orDie.Body != `{"error":"nope"}` {
		t.Fatalf("response = %+v, want the 400 body", orDie)
	}
}

func TestJSONResponseSurfacesAMarshalFailure(t *testing.T) {
	// A channel cannot be marshalled; the error must be returned rather than
	// producing a response with an empty body.
	if _, err := jsonResponse(http.StatusOK, make(chan int)); err == nil {
		t.Fatal("expected a marshal error")
	}
	if _, err := httpJSONResponse(http.StatusOK, make(chan int)); err == nil {
		t.Fatal("expected a marshal error from httpJSONResponse")
	}
}

func TestJSONResponseOrDiePanicsOnAMarshalFailure(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for an unmarshallable payload")
		}
	}()
	jsonResponseOrDie(http.StatusOK, make(chan int))
}

func TestDecodeBase64Body(t *testing.T) {
	got, err := decodeBase64Body("b2ns") // "oi\xd9" is irrelevant; any valid input works
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected decoded bytes")
	}
	if _, err := decodeBase64Body("not base64!!!"); err == nil {
		t.Fatal("expected an error for invalid base64")
	}
}

func TestValidSignature(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account"}`)
	sig := signBytes(body, "secret")

	if !validSignature(body, sig, "secret") {
		t.Fatal("a correctly signed body must validate")
	}
	// Meta prefixes the digest; both forms must be accepted the same way.
	if !validSignature(body, "sha256="+sig[len("sha256="):], "secret") {
		t.Fatal("the sha256= prefix must be tolerated")
	}
	if validSignature(body, sig, "outro-secret") {
		t.Fatal("a signature from a different secret must not validate")
	}
	if validSignature([]byte(`{"tampered":true}`), sig, "secret") {
		t.Fatal("a tampered body must not validate")
	}
	if validSignature(body, "", "secret") {
		t.Fatal("an empty signature must not validate")
	}
}

func TestHandleMarksInboundSessionAndRepliesOnSuccess(t *testing.T) {
	client := &fakeWhatsAppClient{}
	sessions := wasession.NewInMemoryStore()
	app := New(orchestrator.NewService(orchestrator.Config{}), client, "secret", "verify", sessions)

	req := Request{UserID: "5511999", MessageID: "wamid.OK", PhoneNumberID: "phone-1", Text: "oi"}
	_, status, err := app.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if client.markAsReadCalls != 1 || client.sendReplyCalls != 1 {
		t.Fatalf("markAsRead=%d sendReply=%d, want 1 of each", client.markAsReadCalls, client.sendReplyCalls)
	}

	// An inbound message opens the 24h window the notifier later depends on.
	active, err := sessions.Active(context.Background(), "5511999", time.Now())
	if err != nil {
		t.Fatalf("session active: %v", err)
	}
	if !active {
		t.Fatal("handling an inbound message must open the customer-service window")
	}
}

func TestHandleRejectsAMalformedTimestamp(t *testing.T) {
	app := New(orchestrator.NewService(orchestrator.Config{}), &fakeWhatsAppClient{}, "secret", "verify", nil)

	_, status, err := app.Handle(context.Background(), Request{
		UserID: "u1", MessageID: "m1", Text: "oi", Timestamp: "ontem",
	})
	if err == nil {
		t.Fatal("expected an error for an unparseable timestamp")
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
}

// erroringWhatsAppClient fails every send, to exercise the log-and-continue
// paths that must not turn a successful turn into a retry.
type erroringWhatsAppClient struct{ calls int }

func (e *erroringWhatsAppClient) MarkAsRead(context.Context, string, string) error {
	return errors.New("mark as read failed")
}

func (e *erroringWhatsAppClient) SendReply(context.Context, string, string, string, string) error {
	e.calls++
	return errors.New("send reply failed")
}

func (e *erroringWhatsAppClient) SendText(context.Context, string, string, string) error {
	e.calls++
	return errors.New("send text failed")
}
