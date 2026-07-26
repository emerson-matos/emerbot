package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// LocalClient posts replies to the wa-simulator instead of Meta's Graph API —
// what `make demo` runs against.
type LocalClient struct {
	replyURL string
	// client is the transport seam. It replaces a package-level http.Post
	// variable, which every test had to swap globally and therefore could not
	// safely run in parallel with any other test that sent a message.
	client *http.Client
}

func (c *LocalClient) httpClient() *http.Client {
	if c.client != nil {
		return c.client
	}
	return http.DefaultClient
}

func (c *LocalClient) MarkAsRead(context.Context, string, string) error {
	return nil // the simulator has no read receipts
}

func (c *LocalClient) SendReply(ctx context.Context, _ /*phoneNumberID*/, _ /*to*/, messageBody, replyToMessageID string) error {
	return c.post(ctx, replyToMessageID, messageBody)
}

func (c *LocalClient) SendText(ctx context.Context, _ /*phoneNumberID*/, to, messageBody string) error {
	return c.post(ctx, to, messageBody)
}

func (c *LocalClient) post(ctx context.Context, to, messageBody string) error {
	payload := map[string]string{
		"to":      to,
		"message": messageBody,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("local marshal: %w", err)
	}
	// Carry the caller's context so a cancelled turn cancels the send; the
	// previous http.Post call dropped it.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.replyURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("local new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("local post: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("local client: unexpected status %d", resp.StatusCode)
	}
	return nil
}
