package wainbound

import (
	"errors"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
)

func testEnvelope() Envelope {
	return Envelope{
		Message: domain.Message{
			UserID:    "5511999999999",
			Text:      "quanto o joão me deve?",
			Timestamp: time.Date(2026, 8, 11, 13, 30, 0, 0, time.UTC),
			MessageID: "wamid.ABC",
		},
		PhoneNumberID: "123456",
	}
}

// The round trip is the contract between the two Lambdas: every field the worker
// needs has to survive the queue, including the one domain.Message does not
// carry.
func TestEnvelopeRoundTrip(t *testing.T) {
	t.Parallel()

	body, err := testEnvelope().Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := Unmarshal(body)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := testEnvelope()
	if got.Message.UserID != want.Message.UserID || got.Message.Text != want.Message.Text ||
		got.Message.MessageID != want.Message.MessageID {
		t.Fatalf("message = %+v, want %+v", got.Message, want.Message)
	}
	if !got.Message.Timestamp.Equal(want.Message.Timestamp) {
		t.Fatalf("timestamp = %v, want %v", got.Message.Timestamp, want.Message.Timestamp)
	}
	// The field that motivates the envelope: without it the worker knows who
	// wrote but not which business number to answer from.
	if got.PhoneNumberID != want.PhoneNumberID {
		t.Fatalf("phone number id = %q, want %q", got.PhoneNumberID, want.PhoneNumberID)
	}
}

func TestUnmarshalRejectsGarbage(t *testing.T) {
	t.Parallel()

	if _, err := Unmarshal([]byte(`{"message":`)); err == nil {
		t.Fatal("expected an error for a truncated body")
	}
}

// Unmarshal deliberately does not validate: the worker needs to tell "this is
// not our JSON" from "this is our JSON, missing a field" — the first is a
// message that should never have been on this queue, the second is a bug in the
// publisher.
func TestUnmarshalDoesNotValidate(t *testing.T) {
	t.Parallel()

	env, err := Unmarshal([]byte(`{"phone_number_id":"123"}`))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := env.Validate(); !errors.Is(err, ErrNoSender) {
		t.Fatalf("Validate = %v, want ErrNoSender", err)
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	if err := testEnvelope().Validate(); err != nil {
		t.Fatalf("a complete envelope must validate: %v", err)
	}

	noSender := testEnvelope()
	noSender.Message.UserID = ""
	if err := noSender.Validate(); !errors.Is(err, ErrNoSender) {
		t.Fatalf("Validate = %v, want ErrNoSender", err)
	}

	// The message id is the FIFO dedup id, the domain's processed key and the
	// message a reply threads onto — three reasons not to let one through.
	noID := testEnvelope()
	noID.Message.MessageID = ""
	if err := noID.Validate(); !errors.Is(err, ErrNoMessageID) {
		t.Fatalf("Validate = %v, want ErrNoMessageID", err)
	}
}
