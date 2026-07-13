package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type LocalClient struct {
	replyURL string
}

func (c *LocalClient) SendReply(_ context.Context, _ /*phoneNumberID*/ string, _ /*to*/ string, messageBody string, replyToMessageID string) error {
	payload := map[string]string{
		"to":      replyToMessageID,
		"message": messageBody,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("local marshal: %w", err)
	}
	resp, err := http.Post(c.replyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("local post: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("local client: unexpected status %d", resp.StatusCode)
	}
	return nil
}
