package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestLocalClientSendReplyPostsExpectedPayload(t *testing.T) {
	t.Parallel()

	originalPost := httpPost
	t.Cleanup(func() { httpPost = originalPost })

	var (
		gotURL         string
		gotContentType string
		gotPayload     map[string]string
	)
	httpPost = func(url, contentType string, body io.Reader) (*http.Response, error) {
		gotURL = url
		gotContentType = contentType
		rawBody, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawBody, &gotPayload); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
		}, nil
	}

	client := &LocalClient{replyURL: "http://local.test/reply"}
	if err := client.SendReply(context.Background(), "phone-1", "user-1", "oi", "msg-1"); err != nil {
		t.Fatalf("SendReply returned error: %v", err)
	}
	if gotURL != "http://local.test/reply" || gotContentType != "application/json" {
		t.Fatalf("unexpected post target: url=%s contentType=%s", gotURL, gotContentType)
	}
	if gotPayload["to"] != "msg-1" || gotPayload["message"] != "oi" {
		t.Fatalf("unexpected payload: %+v", gotPayload)
	}
}

func TestLocalClientMarkAsReadIsNoop(t *testing.T) {
	t.Parallel()

	client := &LocalClient{replyURL: "http://example.com"}
	if err := client.MarkAsRead(context.Background(), "phone-1", "msg-1"); err != nil {
		t.Fatalf("MarkAsRead returned error: %v", err)
	}
}
