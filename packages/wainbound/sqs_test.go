package wainbound

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// fakeSQS records what was sent, which is the whole of what this publisher does:
// the FIFO attributes are invisible in the body and are exactly what the
// architecture rests on.
type fakeSQS struct {
	inputs []*sqs.SendMessageInput
	err    error
}

func (f *fakeSQS) SendMessage(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return &sqs.SendMessageOutput{}, nil
}

func TestPublishSetsTheFIFOAttributes(t *testing.T) {
	t.Parallel()

	client := &fakeSQS{}
	pub := NewSQSPublisherWithClient(client, "https://sqs.local/queue.fifo")

	if err := pub.Publish(context.Background(), testEnvelope()); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(client.inputs) != 1 {
		t.Fatalf("sent %d messages, want 1", len(client.inputs))
	}
	in := client.inputs[0]

	if *in.QueueUrl != "https://sqs.local/queue.fifo" {
		t.Fatalf("queue url = %q", *in.QueueUrl)
	}
	// The group is the phone: one conversation is serialized, different
	// conversations still run in parallel (ADR-028).
	if *in.MessageGroupId != "5511999999999" {
		t.Fatalf("group id = %q, want the sender's phone", *in.MessageGroupId)
	}
	// The dedup id is Meta's message id — the identifier Meta repeats on retry.
	if *in.MessageDeduplicationId != "wamid.ABC" {
		t.Fatalf("dedup id = %q, want the meta message id", *in.MessageDeduplicationId)
	}

	got, err := Unmarshal([]byte(*in.MessageBody))
	if err != nil {
		t.Fatalf("the body must be a readable envelope: %v", err)
	}
	if got.Message.Text != "quanto o joão me deve?" || got.PhoneNumberID != "123456" {
		t.Fatalf("body = %+v, want the envelope as published", got)
	}
}

// Two messages from the same phone share a group, so the queue keeps them in
// order; two phones do not, so they are free to run at the same time.
func TestPublishGroupsByConversation(t *testing.T) {
	t.Parallel()

	client := &fakeSQS{}
	pub := NewSQSPublisherWithClient(client, "q")

	first := testEnvelope()
	second := testEnvelope()
	second.Message.MessageID = "wamid.DEF"
	other := testEnvelope()
	other.Message.UserID = "5511888888888"
	other.Message.MessageID = "wamid.GHI"

	for _, env := range []Envelope{first, second, other} {
		if err := pub.Publish(context.Background(), env); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	if *client.inputs[0].MessageGroupId != *client.inputs[1].MessageGroupId {
		t.Fatal("two messages from the same phone must share a group")
	}
	if *client.inputs[0].MessageGroupId == *client.inputs[2].MessageGroupId {
		t.Fatal("two different phones must not share a group")
	}
}

func TestPublishRefusesAnEnvelopeItCannotAddress(t *testing.T) {
	t.Parallel()

	client := &fakeSQS{}
	pub := NewSQSPublisherWithClient(client, "q")

	env := testEnvelope()
	env.Message.MessageID = ""
	// A FIFO send without a dedup id is rejected by SQS anyway; failing here
	// names the reason instead of surfacing an InvalidParameterValue.
	if err := pub.Publish(context.Background(), env); !errors.Is(err, ErrNoMessageID) {
		t.Fatalf("publish = %v, want ErrNoMessageID", err)
	}
	if len(client.inputs) != 0 {
		t.Fatal("an invalid envelope must not reach the queue")
	}
}

func TestPublishPropagatesTheSendError(t *testing.T) {
	t.Parallel()

	boom := errors.New("sqs is down")
	pub := NewSQSPublisherWithClient(&fakeSQS{err: boom}, "q")

	// The webhook's only promise is "recebi e enfileirei"; a failure to enqueue
	// has to reach it, so it can hand the message back to Meta.
	if err := pub.Publish(context.Background(), testEnvelope()); !errors.Is(err, boom) {
		t.Fatalf("publish = %v, want it to wrap %v", err, boom)
	}
}
