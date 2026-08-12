// Package wainbound carries an inbound WhatsApp message from the webhook to the
// worker (ADR-028).
//
// What travels is the message the webhook already normalized, never Meta's own
// envelope: the SQS tariff bills every 64 KB of payload as a request, so size is
// price, and the worker has no business reparsing a format that was parsed once
// already — the webhook is the only place that should know what Meta's JSON
// looks like.
package wainbound

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/emerson/emerbot/packages/domain"
)

// Envelope is one inbound message, ready to be processed.
//
// PhoneNumberID is not part of domain.Message and rides beside it because the
// worker cannot answer without it: the Graph API addresses a reply by the
// business number that received the message, and by the time the worker runs
// the webhook (which read it off Meta's metadata) is long gone.
type Envelope struct {
	Message       domain.Message `json:"message"`
	PhoneNumberID string         `json:"phone_number_id"`
}

var (
	// ErrNoSender is an envelope nobody could be answered on.
	ErrNoSender = errors.New("wainbound: envelope without a sender")
	// ErrNoMessageID is an envelope that cannot be deduplicated. The message id
	// is three things at once — the FIFO MessageDeduplicationId, the key of the
	// domain's processed mark (ADR-029) and the message a reply threads onto —
	// so an envelope without one is refused at the edge rather than silently
	// processed twice.
	ErrNoMessageID = errors.New("wainbound: envelope without a message id")
)

// Validate reports whether the envelope carries what both ends need. Meta always
// sends both fields; this is here so a bug on the publishing side fails where it
// happens instead of at the Graph API, one queue hop later.
func (e Envelope) Validate() error {
	if e.Message.UserID == "" {
		return ErrNoSender
	}
	if e.Message.MessageID == "" {
		return ErrNoMessageID
	}
	return nil
}

// Marshal encodes the envelope as the queue's message body.
func (e Envelope) Marshal() ([]byte, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("wainbound: marshal envelope: %w", err)
	}
	return body, nil
}

// Unmarshal decodes a queue message body back into an envelope. It does not
// validate: what to do with an envelope the worker cannot act on is the worker's
// call, and it needs to be able to tell "this is not our JSON" from "this is our
// JSON with a field missing".
func Unmarshal(body []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Envelope{}, fmt.Errorf("wainbound: unmarshal envelope: %w", err)
	}
	return env, nil
}
