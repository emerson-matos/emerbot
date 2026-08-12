package waturn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/wainbound"
	"github.com/emerson/emerbot/packages/wasession"
)

// fakeAgent answers, counts and can fail — the three things a turn depends on.
type fakeAgent struct {
	calls int
	reply string
	err   error
	seen  []domain.Message
}

func (f *fakeAgent) HandleMessage(_ context.Context, message domain.Message) (domain.Response, error) {
	f.calls++
	f.seen = append(f.seen, message)
	if f.err != nil {
		return domain.Response{}, f.err
	}
	return domain.Response{Text: f.reply}, nil
}

type fakeReplier struct {
	calls   int
	replies []string
	to      []string
	from    []string
	replyTo []string
	err     error
}

func (f *fakeReplier) SendReply(_ context.Context, phoneNumberID, to, body, replyToMessageID string) error {
	f.calls++
	f.replies = append(f.replies, body)
	f.to = append(f.to, to)
	f.from = append(f.from, phoneNumberID)
	f.replyTo = append(f.replyTo, replyToMessageID)
	return f.err
}

func testEnvelope() wainbound.Envelope {
	return wainbound.Envelope{
		Message: domain.Message{
			UserID:    "5511999999999",
			Text:      "como estamos",
			Timestamp: time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC),
			MessageID: "wamid.ABC",
		},
		PhoneNumberID: "phone-1",
	}
}

func newTurn(agent Agent, wa Replier) (*Worker, *wasession.InMemoryStore) {
	store := wasession.NewInMemoryStore()
	return New(agent, wa, store, DefaultMaxReceives), store
}

func TestProcessAnswersAndMarks(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{reply: "faturamento de hoje: R$ 1.200"}
	wa := &fakeReplier{}
	w, store := newTurn(agent, wa)
	env := testEnvelope()

	if err := w.Process(context.Background(), env, false); err != nil {
		t.Fatalf("process: %v", err)
	}
	if agent.calls != 1 {
		t.Fatalf("agent called %d times, want 1", agent.calls)
	}
	if wa.calls != 1 || wa.replies[0] != "faturamento de hoje: R$ 1.200" {
		t.Fatalf("replies = %v, want the agent's answer", wa.replies)
	}
	// The reply is addressed with what only the envelope carries: the business
	// number it came in on, the sender, and the message it threads onto.
	if wa.from[0] != "phone-1" || wa.to[0] != env.Message.UserID || wa.replyTo[0] != env.Message.MessageID {
		t.Fatalf("addressed from=%q to=%q replyTo=%q", wa.from[0], wa.to[0], wa.replyTo[0])
	}

	// The mark is written at the end, so it is the record of an answered
	// message rather than a reservation (ADR-029).
	done, err := store.Processed(context.Background(), env.Message.MessageID, time.Now())
	if err != nil {
		t.Fatalf("processed: %v", err)
	}
	if !done {
		t.Fatal("an answered message must be marked processed")
	}
}

// The point of the DynamoDB mark: SQS delivery is at-least-once, so the same
// message can arrive twice however good the transport dedup is.
func TestASecondDeliveryIsNotProcessedTwice(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{reply: "oi"}
	wa := &fakeReplier{}
	w, _ := newTurn(agent, wa)
	env := testEnvelope()

	if err := w.Process(context.Background(), env, false); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := w.Process(context.Background(), env, false); err != nil {
		t.Fatalf("a duplicate is not an error — it is nothing to do: %v", err)
	}

	if agent.calls != 1 {
		t.Fatalf("agent called %d times, want 1 — the second delivery must not re-bill Gemini", agent.calls)
	}
	if wa.calls != 1 {
		t.Fatalf("sent %d replies, want 1 — nobody may be answered twice for one question", wa.calls)
	}
}

// A failed turn writes no mark, so the redelivery the queue is about to make is
// a real second chance rather than a duplicate to skip.
func TestAFailedTurnLeavesTheMessageUnmarked(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{err: errors.New("gemini 504")}
	w, store := newTurn(agent, &fakeReplier{})
	env := testEnvelope()

	if err := w.Process(context.Background(), env, false); err == nil {
		t.Fatal("a failed turn must be reported, so the message goes back on the queue")
	}
	done, _ := store.Processed(context.Background(), env.Message.MessageID, time.Now())
	if done {
		t.Fatal("a message that was never answered must not read as processed")
	}
}

// The fallback is the last thing before the DLQ, and only there: apologising
// while a retry is still coming would be an apology for an answer the person is
// about to receive.
func TestTheFallbackOnlyFiresOnTheLastAttempt(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{err: errors.New("gemini 504")}
	wa := &fakeReplier{}
	w, _ := newTurn(agent, wa)

	if err := w.Process(context.Background(), testEnvelope(), false); err == nil {
		t.Fatal("expected the turn to fail")
	}
	if wa.calls != 0 {
		t.Fatalf("sent %d messages on a retriable attempt, want none", wa.calls)
	}

	if err := w.Process(context.Background(), testEnvelope(), true); err == nil {
		t.Fatal("the last attempt still fails — the message belongs in the DLQ")
	}
	if wa.calls != 1 || wa.replies[0] != fallbackReply {
		t.Fatalf("replies = %v, want exactly the fallback", wa.replies)
	}
}

// Failing on the last attempt is deliberate: the person was told, but the
// message must still reach the DLQ, where the failure is visible (ADR-028).
func TestTheLastAttemptStillFails(t *testing.T) {
	t.Parallel()

	boom := errors.New("gemini 504")
	w, _ := newTurn(&fakeAgent{err: boom}, &fakeReplier{})

	if err := w.Process(context.Background(), testEnvelope(), true); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap %v", err, boom)
	}
}

// The answer exists but nobody has it: that is worth another delivery, which is
// exactly what the queue makes cheap to ask for.
func TestAnUnsentReplyIsRetried(t *testing.T) {
	t.Parallel()

	wa := &fakeReplier{err: errors.New("graph api unreachable")}
	w, store := newTurn(&fakeAgent{reply: "oi"}, wa)
	env := testEnvelope()

	if err := w.Process(context.Background(), env, false); err == nil {
		t.Fatal("a reply that never left must fail the delivery")
	}
	done, _ := store.Processed(context.Background(), env.Message.MessageID, time.Now())
	if done {
		t.Fatal("a message whose answer was never delivered must not be marked processed")
	}
}

// An empty answer is a turn that ran: nothing to send, but the message was
// handled and must not be handled again.
func TestAnEmptyAnswerIsStillAProcessedMessage(t *testing.T) {
	t.Parallel()

	wa := &fakeReplier{}
	w, store := newTurn(&fakeAgent{reply: ""}, wa)
	env := testEnvelope()

	if err := w.Process(context.Background(), env, false); err != nil {
		t.Fatalf("process: %v", err)
	}
	if wa.calls != 0 {
		t.Fatalf("sent %d messages for an empty answer, want none", wa.calls)
	}
	if done, _ := store.Processed(context.Background(), env.Message.MessageID, time.Now()); !done {
		t.Fatal("the turn ran, so the message is processed")
	}
}

// The webhook validates before publishing, so an envelope missing a field is
// our bug. It is refused before anything is spent on it.
func TestProcessRefusesAnEnvelopeItCannotAnswer(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{}
	wa := &fakeReplier{}
	w, _ := newTurn(agent, wa)

	env := testEnvelope()
	env.Message.UserID = ""
	if err := w.Process(context.Background(), env, true); !errors.Is(err, wainbound.ErrNoSender) {
		t.Fatalf("error = %v, want ErrNoSender", err)
	}
	if agent.calls != 0 || wa.calls != 0 {
		t.Fatalf("agent=%d sends=%d, want nothing attempted", agent.calls, wa.calls)
	}
}

// A dedup store that cannot answer must not stop a real message: a possible
// duplicate beats a dropped question, which is the same call the webhook made
// when the check lived there.
func TestAnUnreadableDedupStoreDoesNotDropTheMessage(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{reply: "oi"}
	w := New(agent, &fakeReplier{}, brokenStore{}, DefaultMaxReceives)

	if err := w.Process(context.Background(), testEnvelope(), false); err != nil {
		t.Fatalf("process: %v", err)
	}
	if agent.calls != 1 {
		t.Fatalf("agent called %d times, want the message processed anyway", agent.calls)
	}
}

type brokenStore struct{}

func (brokenStore) Processed(context.Context, string, time.Time) (bool, error) {
	return false, errors.New("dynamo is down")
}

func (brokenStore) MarkProcessed(context.Context, string, time.Time) (bool, error) {
	return false, errors.New("dynamo is down")
}

// The agent gets the message as the webhook normalized it, not a re-parse of
// Meta's envelope: the text, the sender and the message's own timestamp (which
// every "hoje" in the analysis is measured against).
func TestTheAgentSeesTheNormalizedMessage(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{reply: "ok"}
	w, _ := newTurn(agent, &fakeReplier{})
	env := testEnvelope()

	if err := w.Process(context.Background(), env, false); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := agent.seen[0]; got != env.Message {
		t.Fatalf("agent saw %+v, want %+v", got, env.Message)
	}
}
