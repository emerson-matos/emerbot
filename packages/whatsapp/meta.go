package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const whatsappBaseURL = "https://graph.facebook.com/v25.0"

type metaReplyPayload struct {
	MessagingProduct string       `json:"messaging_product"`
	To               string       `json:"to"`
	Text             metaTextBody `json:"text"`
	Context          metaContext  `json:"context,omitempty"`
}

// metaTextPayload is a message with no reply context — Meta rejects a
// context object carrying an empty message_id, so proactive sends use this
// leaner shape instead of metaReplyPayload.
type metaTextPayload struct {
	MessagingProduct string       `json:"messaging_product"`
	To               string       `json:"to"`
	Text             metaTextBody `json:"text"`
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
	payload := metaReadPayload{
		MessagingProduct: "whatsapp",
		Status:           "read",
		MessageID:        messageID,
	}

	_, err := c.postJSON(ctx, phoneNumberID, payload, http.StatusOK)
	if err != nil {
		return fmt.Errorf("meta mark as read: %w", err)
	}
	return nil
}

func (c *MetaClient) SendReply(ctx context.Context, phoneNumberID, to, messageBody, replyToMessageID string) error {
	payload := metaReplyPayload{
		MessagingProduct: "whatsapp",
		To:               to,
		Text:             metaTextBody{Body: messageBody},
		Context:          metaContext{MessageID: replyToMessageID},
	}

	_, err := c.postJSON(ctx, phoneNumberID, payload, http.StatusOK, http.StatusCreated)
	if err != nil {
		return fmt.Errorf("meta send reply: %w", err)
	}
	return nil
}

func (c *MetaClient) SendText(ctx context.Context, phoneNumberID, to, messageBody string) error {
	payload := metaTextPayload{
		MessagingProduct: "whatsapp",
		To:               to,
		Text:             metaTextBody{Body: messageBody},
	}

	_, err := c.postJSON(ctx, phoneNumberID, payload, http.StatusOK, http.StatusCreated)
	if err != nil {
		return fmt.Errorf("meta send text: %w", err)
	}
	return nil
}

func (c *MetaClient) postJSON(ctx context.Context, phoneNumberID string, payload any, expectedStatus ...int) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/messages", whatsappBaseURL, phoneNumberID)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("meta marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("meta new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meta post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("meta read response: %w", readErr)
	}

	for _, status := range expectedStatus {
		if resp.StatusCode == status {
			return respBody, nil
		}
	}

	return nil, fmt.Errorf("status=%d body=%s", resp.StatusCode, string(respBody))
}
