package whatsapp

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestNewClientFromEnvPicksTheRightTransport(t *testing.T) {
	t.Run("a token selects the Meta client", func(t *testing.T) {
		c := NewClientFromEnv("graph-token")
		meta, ok := c.(*MetaClient)
		if !ok {
			t.Fatalf("client = %T, want *MetaClient when a Graph API token is set", c)
		}
		if meta.token != "graph-token" {
			t.Fatalf("token = %q, want the supplied token", meta.token)
		}
	})

	t.Run("no token falls back to the simulator", func(t *testing.T) {
		t.Setenv("REPLY_URL", "http://sim.test/reply")
		c := NewClientFromEnv("")
		local, ok := c.(*LocalClient)
		if !ok {
			t.Fatalf("client = %T, want *LocalClient with no token", c)
		}
		if local.replyURL != "http://sim.test/reply" {
			t.Fatalf("replyURL = %q, want the REPLY_URL value", local.replyURL)
		}
	})

	t.Run("no token and no REPLY_URL uses the compose default", func(t *testing.T) {
		c := NewClientFromEnv("")
		local, ok := c.(*LocalClient)
		if !ok {
			t.Fatalf("client = %T, want *LocalClient", c)
		}
		if local.replyURL != "http://wa-simulator:9000/reply" {
			t.Fatalf("replyURL = %q, want the podman compose default", local.replyURL)
		}
	})

	t.Run("a schemeless REPLY_URL yields no client", func(t *testing.T) {
		// A misconfigured URL must not silently produce a client that posts
		// nowhere — the caller has to notice.
		t.Setenv("REPLY_URL", "not a url")
		if c := NewClientFromEnv(""); c != nil {
			t.Fatalf("client = %#v, want nil for an unusable REPLY_URL", c)
		}
	})
}

func TestNewLocalClientAndNewMetaClient(t *testing.T) {
	local, ok := NewLocalClient("http://x/reply").(*LocalClient)
	if !ok || local.replyURL != "http://x/reply" || local.client == nil {
		t.Fatalf("NewLocalClient built %+v, want a LocalClient with a URL and an http client", local)
	}

	meta, ok := NewMetaClient("tok").(*MetaClient)
	if !ok || meta.token != "tok" || meta.client == nil {
		t.Fatalf("NewMetaClient built %+v, want a MetaClient with a token and an http client", meta)
	}
}

func TestNewMetaClientWithClient(t *testing.T) {
	custom := &http.Client{}
	if got := NewMetaClientWithClient("tok", custom); got.client != custom {
		t.Fatal("the supplied http client must be the one used")
	}
	// A nil client is a caller mistake that must not panic on first send.
	if got := NewMetaClientWithClient("tok", nil); got.client != http.DefaultClient {
		t.Fatal("a nil client must fall back to http.DefaultClient")
	}
}

func TestMetaClientSendText(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{responseStatus: http.StatusOK, responseBody: `{}`}
	client := NewMetaClientWithClient("token-123", &http.Client{Transport: transport})

	if err := client.SendText(context.Background(), "phone-1", "5511999", "lembrete"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if transport.request == nil {
		t.Fatal("no request was issued")
	}
	body := transport.body
	// SendText must not carry a reply context: Meta rejects a context object
	// with an empty message_id, which is why it uses a leaner payload.
	if strings.Contains(body, "context") {
		t.Fatalf("body = %s, want no context object on a standalone send", body)
	}
	for _, want := range []string{`"to":"5511999"`, `"body":"lembrete"`, `"messaging_product":"whatsapp"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %s, want it to contain %s", body, want)
		}
	}
}

func TestMetaClientSendTextReportsAnAPIError(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{responseStatus: http.StatusUnauthorized, responseBody: `{"error":"bad token"}`}
	client := NewMetaClientWithClient("token-123", &http.Client{Transport: transport})

	err := client.SendText(context.Background(), "phone-1", "5511999", "oi")
	if err == nil {
		t.Fatal("expected an error for a non-2xx response")
	}
	// The status and body must both survive into the error, or a failed send
	// is undiagnosable from the logs.
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("error = %v, want it to carry the status and body", err)
	}
}
