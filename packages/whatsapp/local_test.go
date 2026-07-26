package whatsapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// localServer records what the client posted and answers with status.
type localServer struct {
	*httptest.Server
	path        string
	contentType string
	payload     map[string]string
}

func newLocalServer(t *testing.T, status int) *localServer {
	t.Helper()
	rec := &localServer{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.contentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&rec.payload) //nolint:errcheck
		w.WriteHeader(status)
	}))
	t.Cleanup(rec.Close)
	return rec
}

func newLocalClient(srv *localServer) *LocalClient {
	return &LocalClient{replyURL: srv.URL + "/reply", client: srv.Client()}
}

func TestLocalClientSendReplyPostsExpectedPayload(t *testing.T) {
	t.Parallel()

	srv := newLocalServer(t, http.StatusOK)
	if err := newLocalClient(srv).SendReply(context.Background(), "phone-1", "user-1", "oi", "msg-1"); err != nil {
		t.Fatalf("SendReply returned error: %v", err)
	}

	if srv.path != "/reply" || srv.contentType != "application/json" {
		t.Fatalf("posted to %s with content type %q, want /reply as JSON", srv.path, srv.contentType)
	}
	// A reply is addressed to the message it answers, not to the phone.
	if srv.payload["to"] != "msg-1" || srv.payload["message"] != "oi" {
		t.Fatalf("payload = %+v, want to=msg-1 message=oi", srv.payload)
	}
}

func TestLocalClientSendTextAddressesThePhone(t *testing.T) {
	t.Parallel()

	srv := newLocalServer(t, http.StatusOK)
	if err := newLocalClient(srv).SendText(context.Background(), "phone-1", "5511999", "lembrete"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}

	// SendText has no inbound message to reply to, so it addresses the number.
	if srv.payload["to"] != "5511999" || srv.payload["message"] != "lembrete" {
		t.Fatalf("payload = %+v, want to=5511999 message=lembrete", srv.payload)
	}
}

func TestLocalClientToleratesANonOKStatus(t *testing.T) {
	t.Parallel()

	// The simulator is a dev convenience; a bad status is logged, not fatal.
	srv := newLocalServer(t, http.StatusInternalServerError)
	if err := newLocalClient(srv).SendText(context.Background(), "phone-1", "5511999", "oi"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
}

func TestLocalClientReportsATransportFailure(t *testing.T) {
	t.Parallel()

	srv := newLocalServer(t, http.StatusOK)
	client := newLocalClient(srv)
	srv.Close() // nothing listening any more

	if err := client.SendText(context.Background(), "phone-1", "5511999", "oi"); err == nil {
		t.Fatal("expected an error when the simulator is unreachable")
	}
}

func TestLocalClientHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	srv := newLocalServer(t, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The context used to be dropped entirely, so a cancelled turn still sent.
	if err := newLocalClient(srv).SendText(ctx, "phone-1", "5511999", "oi"); err == nil {
		t.Fatal("expected a cancelled context to abort the send")
	}
}

func TestLocalClientMarkAsReadIsNoop(t *testing.T) {
	t.Parallel()

	client := &LocalClient{replyURL: "http://example.com"}
	if err := client.MarkAsRead(context.Background(), "phone-1", "msg-1"); err != nil {
		t.Fatalf("MarkAsRead returned error: %v", err)
	}
}

func TestLocalClientFallsBackToTheDefaultHTTPClient(t *testing.T) {
	t.Parallel()

	// A zero-value LocalClient (no injected client) must still be usable.
	if got := (&LocalClient{}).httpClient(); got != http.DefaultClient {
		t.Fatalf("httpClient() = %v, want http.DefaultClient", got)
	}
}
