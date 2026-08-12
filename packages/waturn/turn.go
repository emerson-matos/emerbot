// Package waturn processes one inbound WhatsApp message: the idempotency check,
// the agent turn (history, Gemini, tools) and the reply (ADR-028).
//
// It is the half of the old webhook that takes time. The webhook now answers
// Meta in ~50ms and this runs behind a FIFO queue, so the person's answer and
// Meta's ACK stopped being the same event.
//
// It lives in packages/ rather than under apps/worker because two binaries run
// it: the worker Lambda (apps/worker/cmd/lambda, through HandleSQS) and the
// local webhook, which has no queue in front of it and calls Process inline so
// `make demo` still answers a message end to end.
//
// # What the queue guarantees, and what it does not
//
// Messages are grouped by phone (MessageGroupId), and SQS FIFO keeps only one
// batch of a group in flight at a time. So:
//
//   - two messages from the same phone are processed in the order they were
//     sent, one after the other — which is what the agent needs, since it reads
//     the conversation history as input;
//   - two different phones are not ordered against each other and run in
//     parallel, which is the parallelism a conversation bot wants;
//   - nothing is ordered against the outside world: the reply to a message is
//     sent before the next message of the group starts, but a person can always
//     type again while the first turn is still running;
//   - a message that keeps failing blocks its own group until it is moved to
//     the DLQ (head-of-line blocking). That is the price of ordering, and it is
//     bounded by maxReceiveCount × the visibility timeout.
//
// Delivery is at-least-once, so none of the above makes processing idempotent —
// that is the mark in DynamoDB (ADR-029), asked before the turn and written
// after it.
package waturn

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/wainbound"
)

// Agent turns an inbound message into what to say back. orchestrator.Service
// satisfies it; the worker has no business knowing about the other seventeen
// things that service can do.
type Agent interface {
	HandleMessage(ctx context.Context, message domain.Message) (domain.Response, error)
}

// Replier is the one WhatsApp call the worker makes. MarkAsRead stays in the
// webhook, where the message is still fresh.
type Replier interface {
	SendReply(ctx context.Context, phoneNumberID, to, messageBody, replyToMessageID string) error
}

// ProcessedStore answers and then records the domain's dedup question.
// packages/wasession satisfies it; the 24h-window half of that store is the
// webhook's business, not the worker's.
type ProcessedStore interface {
	Processed(ctx context.Context, messageID string, now time.Time) (bool, error)
	MarkProcessed(ctx context.Context, messageID string, now time.Time) (bool, error)
}

// Worker holds the turn's collaborators. Everything it needs is behind an
// interface, so the whole path below is exercised without AWS or Gemini.
type Worker struct {
	agent     Agent
	wa        Replier
	processed ProcessedStore
	// maxReceives mirrors the queue's redrive policy so the worker can tell it
	// is on the last attempt. See HandleSQS.
	maxReceives int
	now         func() time.Time
}

func New(agent Agent, wa Replier, processed ProcessedStore, maxReceives int) *Worker {
	if maxReceives < 1 {
		maxReceives = DefaultMaxReceives
	}
	return &Worker{
		agent:       agent,
		wa:          wa,
		processed:   processed,
		maxReceives: maxReceives,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// fallbackReply is what the user hears when the turn could not be completed and
// no further delivery is coming.
//
// It does not name a cause: the same silence came from a Lambda killed at its
// timeout, from Gemini returning 504, and from a tool erroring, and guessing
// wrong in front of someone is worse than not guessing. What it does carry is
// the one action that actually helps — a smaller question fits in the budget
// that a big one did not.
const fallbackReply = "Não consegui montar essa resposta agora. " +
	"Manda de novo daqui a pouco — se for uma pergunta grande, quebrar em duas costuma passar."

// Process runs one message end to end.
//
// lastAttempt says no redelivery follows this one, which is the only moment the
// user is told the turn failed: before that, a retry is likely to succeed and an
// apology would be noise; after it, silence is all they would ever get. Failing
// on the last attempt is still a failure — the message goes to the DLQ, where it
// is visible, instead of being quietly deleted because we apologised.
func (w *Worker) Process(ctx context.Context, env wainbound.Envelope, lastAttempt bool) error {
	if err := env.Validate(); err != nil {
		// The webhook validates before publishing, so this is our bug, not a
		// transient failure. Let it reach the DLQ with the body attached rather
		// than disappearing into a log line.
		return err
	}

	now := w.now()
	msgID := env.Message.MessageID

	done, err := w.processed.Processed(ctx, msgID, now)
	if err != nil {
		// Best effort, as before: a store that cannot answer must not stop a
		// real message. A possible duplicate beats a dropped question.
		log.Printf("dedup check message_id=%s: %v", msgID, err)
	} else if done {
		log.Printf("ignoring already processed message_id=%s", msgID)
		return nil
	}

	response, err := w.agent.HandleMessage(ctx, env.Message)
	if err != nil {
		log.Printf("processing message %s: %v", msgID, err)
		if lastAttempt {
			// Say so, rather than going quiet. A message that gets no reply is
			// indistinguishable from one that never arrived, and the person is
			// left deciding alone whether to type it again.
			if serr := w.reply(ctx, env, fallbackReply); serr != nil {
				log.Printf("send fallback message_id=%s: %v", msgID, serr)
			}
		}
		return fmt.Errorf("agent turn for message %s: %w", msgID, err)
	}

	if response.Text != "" {
		if serr := w.reply(ctx, env, response.Text); serr != nil {
			// The answer exists but nobody has it, so this is worth another
			// delivery — the queue is what makes that free to ask for.
			return fmt.Errorf("send reply for message %s: %w", msgID, serr)
		}
	}

	// The turn is over, so the mark is a fact now (ADR-029). A failure to write
	// it is logged and not propagated: retrying would answer the same question
	// twice, which is exactly what the mark exists to prevent.
	if first, merr := w.processed.MarkProcessed(ctx, msgID, now); merr != nil {
		log.Printf("mark processed message_id=%s: %v", msgID, merr)
	} else if !first {
		log.Printf("message_id=%s was marked processed by a concurrent delivery", msgID)
	}
	return nil
}

func (w *Worker) reply(ctx context.Context, env wainbound.Envelope, text string) error {
	if w.wa == nil {
		return nil
	}
	return w.wa.SendReply(ctx, env.PhoneNumberID, env.Message.UserID, text, env.Message.MessageID)
}
