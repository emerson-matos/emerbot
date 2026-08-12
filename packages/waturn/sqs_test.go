package waturn

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/aws/aws-lambda-go/events"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/wainbound"
	"github.com/emerson/emerbot/packages/wasession"
)

func sqsRecord(t *testing.T, env wainbound.Envelope, receiveCount int) events.SQSMessage {
	t.Helper()
	body, err := env.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return events.SQSMessage{
		MessageId: env.Message.MessageID,
		Body:      string(body),
		// SQS carries both of these as message system attributes: the group is
		// the phone (the queue's ordering unit) and the receive count is what
		// tells the worker it is on the last attempt.
		Attributes: map[string]string{
			receiveCountAttr: strconv.Itoa(receiveCount),
			"MessageGroupId": env.Message.UserID,
		},
	}
}

func TestHandleSQSProcessesTheBatch(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{reply: "oi"}
	wa := &fakeReplier{}
	w, _ := newTurn(agent, wa)

	second := testEnvelope()
	second.Message.MessageID = "wamid.DEF"

	err := w.HandleSQS(context.Background(), events.SQSEvent{Records: []events.SQSMessage{
		sqsRecord(t, testEnvelope(), 1),
		sqsRecord(t, second, 1),
	}})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if agent.calls != 2 {
		t.Fatalf("agent called %d times, want both records processed", agent.calls)
	}
}

// The fallback exists because the worker can see the receive count. On the last
// delivery there is no next one, so the person hears about it here rather than
// from a Lambda hanging off the DLQ (ADR-028).
func TestHandleSQSTellsTheUserOnTheLastReceive(t *testing.T) {
	t.Parallel()

	wa := &fakeReplier{}
	w, _ := newTurn(&fakeAgent{err: errors.New("gemini 504")}, wa)

	for receive := 1; receive < DefaultMaxReceives; receive++ {
		if err := w.HandleSQS(context.Background(), events.SQSEvent{
			Records: []events.SQSMessage{sqsRecord(t, testEnvelope(), receive)},
		}); err == nil {
			t.Fatalf("receive %d: expected the failure to be reported", receive)
		}
		if wa.calls != 0 {
			t.Fatalf("receive %d sent %d messages, want silence while a retry is coming", receive, wa.calls)
		}
	}

	if err := w.HandleSQS(context.Background(), events.SQSEvent{
		Records: []events.SQSMessage{sqsRecord(t, testEnvelope(), DefaultMaxReceives)},
	}); err == nil {
		t.Fatal("the last receive still fails, so the message reaches the DLQ")
	}
	if wa.calls != 1 || wa.replies[0] != fallbackReply {
		t.Fatalf("replies = %v, want exactly the fallback on the last receive", wa.replies)
	}
}

// The queue's redrive policy is the source of truth for how many deliveries
// there are; the Lambda is given that number, so a change to the policy cannot
// leave the fallback firing on the wrong attempt.
func TestTheLastAttemptFollowsTheConfiguredRedrivePolicy(t *testing.T) {
	t.Parallel()

	wa := &fakeReplier{}
	w := New(&fakeAgent{err: errors.New("gemini 504")}, wa, wasession.NewInMemoryStore(), 2)

	if err := w.HandleSQS(context.Background(), events.SQSEvent{
		Records: []events.SQSMessage{sqsRecord(t, testEnvelope(), 2)},
	}); err == nil {
		t.Fatal("expected the failure to be reported")
	}
	if wa.calls != 1 {
		t.Fatalf("sent %d messages on receive 2 of 2, want the fallback", wa.calls)
	}
}

// An absent or unreadable count reads as "not the last attempt": the cost of
// guessing wrong the other way is an apology for an answer that then arrives.
func TestAnUnreadableReceiveCountIsNeverTheLastAttempt(t *testing.T) {
	t.Parallel()

	wa := &fakeReplier{}
	w, _ := newTurn(&fakeAgent{err: errors.New("gemini 504")}, wa)

	record := sqsRecord(t, testEnvelope(), 1)
	record.Attributes[receiveCountAttr] = "muitas"
	if err := w.HandleSQS(context.Background(), events.SQSEvent{Records: []events.SQSMessage{record}}); err == nil {
		t.Fatal("expected the failure to be reported")
	}

	noAttrs := sqsRecord(t, testEnvelope(), 1)
	noAttrs.Attributes = nil
	if err := w.HandleSQS(context.Background(), events.SQSEvent{Records: []events.SQSMessage{noAttrs}}); err == nil {
		t.Fatal("expected the failure to be reported")
	}

	if wa.calls != 0 {
		t.Fatalf("sent %d messages, want none — the attempt count was not readable", wa.calls)
	}
}

// A batch stops at its first failure so the group's order is kept, and the
// redelivery of the whole batch does not redo what already succeeded — the
// mark in DynamoDB is what makes re-reading the batch safe.
func TestABatchStopsAtTheFirstFailureAndDoesNotRedoTheRest(t *testing.T) {
	t.Parallel()

	failing := &failOnce{reply: "oi", failFor: "wamid.DEF"}
	wa := &fakeReplier{}
	w, _ := newTurn(failing, wa)

	first := testEnvelope()
	second := testEnvelope()
	second.Message.MessageID = "wamid.DEF"
	third := testEnvelope()
	third.Message.MessageID = "wamid.GHI"

	batch := events.SQSEvent{Records: []events.SQSMessage{
		sqsRecord(t, first, 1), sqsRecord(t, second, 1), sqsRecord(t, third, 1),
	}}

	if err := w.HandleSQS(context.Background(), batch); err == nil {
		t.Fatal("expected the batch to report the failure")
	}
	if failing.answered != 1 {
		t.Fatalf("answered %d messages, want only the one before the failure", failing.answered)
	}

	// Redelivery: the first message is skipped, the second gets its retry, the
	// third is finally reached.
	failing.failFor = ""
	if err := w.HandleSQS(context.Background(), batch); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if wa.calls != 3 {
		t.Fatalf("sent %d replies across both deliveries, want 3 — one per message", wa.calls)
	}
}

type failOnce struct {
	reply    string
	failFor  string
	answered int
}

func (f *failOnce) HandleMessage(_ context.Context, message domain.Message) (domain.Response, error) {
	if message.MessageID == f.failFor {
		return domain.Response{}, errors.New("gemini 504")
	}
	f.answered++
	return domain.Response{Text: f.reply}, nil
}

func TestHandleSQSRejectsABodyItCannotRead(t *testing.T) {
	t.Parallel()

	w, _ := newTurn(&fakeAgent{}, &fakeReplier{})
	err := w.HandleSQS(context.Background(), events.SQSEvent{Records: []events.SQSMessage{
		{MessageId: "sqs-1", Body: `{"message":`},
	}})
	if err == nil {
		t.Fatal("a body that is not an envelope must be reported, not dropped")
	}
}
