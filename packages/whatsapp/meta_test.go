package whatsapp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMetaClientMarkAsReadSuccess(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{
		responseStatus: http.StatusOK,
		responseBody:   `{"success":true}`,
	}
	client := &MetaClient{
		token: "token-123",
		client: &http.Client{
			Transport: transport,
		},
	}

	err := client.MarkAsRead(context.Background(), "phone-1", "msg-1")
	if err != nil {
		t.Fatalf("MarkAsRead returned error: %v", err)
	}
	if transport.request == nil {
		t.Fatal("expected request to be captured")
	}
	if transport.request.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", transport.request.Method)
	}
	if got := transport.request.Header.Get("Authorization"); got != "Bearer token-123" {
		t.Fatalf("expected bearer token header, got %s", got)
	}
	if !strings.Contains(transport.body, `"status":"read"`) {
		t.Fatalf("expected read payload, got %s", transport.body)
	}
	if !strings.Contains(transport.request.URL.String(), "/phone-1/messages") {
		t.Fatalf("expected phone number path in URL, got %s", transport.request.URL.String())
	}
}

func TestMetaClientSendReplyAcceptsCreated(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{
		responseStatus: http.StatusCreated,
		responseBody:   `{"messages":[{"id":"wamid.1"}]}`,
	}
	client := &MetaClient{
		token: "token-123",
		client: &http.Client{
			Transport: transport,
		},
	}

	err := client.SendReply(context.Background(), "phone-1", "5511999999999", "oi", "msg-1")
	if err != nil {
		t.Fatalf("SendReply returned error: %v", err)
	}
	if !strings.Contains(transport.body, `"to":"5511999999999"`) {
		t.Fatalf("expected recipient in payload, got %s", transport.body)
	}
	if !strings.Contains(transport.body, `"message_id":"msg-1"`) {
		t.Fatalf("expected reply context in payload, got %s", transport.body)
	}
}

func TestMetaClientSendReplyReturnsMetaResponseBodyOnError(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{
		responseStatus: http.StatusBadRequest,
		responseBody:   `{"error":{"message":"invalid recipient"}}`,
	}
	client := &MetaClient{
		token: "token-123",
		client: &http.Client{
			Transport: transport,
		},
	}

	err := client.SendReply(context.Background(), "phone-1", "bad-user", "oi", "msg-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "meta send reply") {
		t.Fatalf("expected send reply prefix, got %v", err)
	}
	if !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected status in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid recipient") {
		t.Fatalf("expected response body in error, got %v", err)
	}
}

func TestMetaClientMarkAsReadReturnsNetworkError(t *testing.T) {
	t.Parallel()

	client := &MetaClient{
		token: "token-123",
		client: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("boom")
			}),
		},
	}

	err := client.MarkAsRead(context.Background(), "phone-1", "msg-1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "meta mark as read") {
		t.Fatalf("expected mark as read prefix, got %v", err)
	}
	if !strings.Contains(err.Error(), "meta post:") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected network error in message, got %v", err)
	}
}

type captureTransport struct {
	request        *http.Request
	body           string
	responseStatus int
	responseBody   string
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.request = req.Clone(req.Context())
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	t.body = string(body)

	return &http.Response{
		StatusCode: t.responseStatus,
		Body:       io.NopCloser(strings.NewReader(t.responseBody)),
		Header:     make(http.Header),
	}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
