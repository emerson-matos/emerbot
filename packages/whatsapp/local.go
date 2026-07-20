package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

var httpPost = http.Post

type LocalClient struct {
	replyURL string
}

func (c *LocalClient) MarkAsRead(ctx context.Context, phoneNumberID string, messageID string) error {
	return nil
}

func (c *LocalClient) SendReply(_ context.Context, _ /*phoneNumberID*/ string, _ /*to*/ string, messageBody string, replyToMessageID string) error {
	return c.post(replyToMessageID, messageBody)
}

func (c *LocalClient) SendText(_ context.Context, _ /*phoneNumberID*/ string, to string, messageBody string) error {
	return c.post(to, messageBody)
}

func (c *LocalClient) post(to, messageBody string) error {
	payload := map[string]string{
		"to":      to,
		"message": messageBody,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("local marshal: %w", err)
	}
	resp, err := httpPost(c.replyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("local post: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("local client: unexpected status %d", resp.StatusCode)
	}
	return nil
}
