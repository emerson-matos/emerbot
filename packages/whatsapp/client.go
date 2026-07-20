package whatsapp

import (
	"context"
	"net/http"
	"net/url"

	"github.com/emerson/emerbot/packages/shared"
)

type Client interface {
	MarkAsRead(ctx context.Context, phoneNumberID, messageID string) error
	SendReply(ctx context.Context, phoneNumberID, to, messageBody, replyToMessageID string) error
	// SendText sends a standalone message (no reply context) — used by the
	// scheduled notifier, which has no inbound message to reply to.
	SendText(ctx context.Context, phoneNumberID, to, messageBody string) error
}

func NewLocalClient(replyURL string) Client {
	return &LocalClient{replyURL: replyURL}
}

func NewMetaClient(graphAPIToken string) Client {
	return &MetaClient{token: graphAPIToken, client: http.DefaultClient}
}

func NewClientFromEnv(graphAPIToken string) Client {
	if graphAPIToken != "" {
		return NewMetaClient(graphAPIToken)
	}
	replyURL := shared.Getenv("REPLY_URL", "http://wa-simulator:9000/reply")
	if u, err := url.Parse(replyURL); err == nil && u.Scheme != "" {
		return NewLocalClient(replyURL)
	}
	return nil
}
