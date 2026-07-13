package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

const whatsappBaseURL = "https://graph.facebook.com/v25.0"

type metaReplyPayload struct {
	MessagingProduct string             `json:"messaging_product"`
	To               string             `json:"to"`
	Text             metaTextBody       `json:"text"`
	Context          metaContext        `json:"context,omitempty"`
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

func NewMetaClientWithClient(token string, client *http.Client) *MetaClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &MetaClient{token: token, client: client}
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
	if err != nil {
		return fmt.Errorf("meta post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Printf("meta client: unexpected status %d", resp.StatusCode)
	}
	return nil
}
