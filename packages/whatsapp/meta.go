package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

const whatsappBaseURL = "https://graph.facebook.com/v25.0"

type metaReplyPayload struct {
	MessagingProduct string       `json:"messaging_product"`
	To               string       `json:"to"`
	Text             metaTextBody `json:"text"`
	Context          metaContext  `json:"context,omitempty"`
}

type metaTextBody struct {
	Body string `json:"body"`
}

type metaContext struct {
	MessageID string `json:"message_id"`
}

type MetaClient struct {
	token  string
	client *http.Client
}

type metaReadPayload struct {
	MessagingProduct string `json:"messaging_product"`
	Status           string `json:"status"`
	MessageID        string `json:"message_id"`
}

func NewMetaClientWithClient(token string) *MetaClient {
	return &MetaClient{token: token, client: http.DefaultClient}
}

func (c *MetaClient) MarkAsRead(ctx context.Context, phoneNumberID, messageID string) error {
	url := fmt.Sprintf("%s/%s/messages", whatsappBaseURL, phoneNumberID)

	payload := metaReadPayload{
		MessagingProduct: "whatsapp",
		Status:           "read",
		MessageID:        messageID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("meta marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("meta new request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("meta post: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"meta mark as read: status=%d body=%s",
			resp.StatusCode,
			respBody,
		)
	}

	return nil
}

func (c *MetaClient) SendReply(_ context.Context, phoneNumberID, to, messageBody, replyToMessageID string) error {
	url := fmt.Sprintf("%s/%s/messages", whatsappBaseURL, phoneNumberID)
	payload := metaReplyPayload{
		MessagingProduct: "whatsapp",
		To:               to,
		Text:             metaTextBody{Body: messageBody},
		Context:          metaContext{MessageID: replyToMessageID},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("meta marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("meta new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	defer func() { _ = resp.Body.Close() }()
	if err != nil {
		return fmt.Errorf("meta post: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Printf(
			"meta status=%d body=%s",
			resp.StatusCode,
			respBody,
		)
	}
	return nil
}
