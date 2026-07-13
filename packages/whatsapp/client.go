package whatsapp

import (
	"context"
	"net/url"

	"github.com/emerson/emerbot/packages/shared"
)

type Client interface {
	SendReply(ctx context.Context, phoneNumberID, to, messageBody, replyToMessageID string) error
}

func NewLocalClient(replyURL string) Client {
	return &LocalClient{replyURL: replyURL}
}

func NewMetaClient(graphAPIToken string) Client {
	return &MetaClient{token: graphAPIToken}
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
