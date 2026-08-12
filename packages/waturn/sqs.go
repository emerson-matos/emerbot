package waturn

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/aws/aws-lambda-go/events"

	"github.com/emerson/emerbot/packages/wainbound"
)

// DefaultMaxReceives mirrors the queue's redrive policy (maxReceiveCount = 5,
// see infra/modules/api_gateway_lambda). It is a default and not the source of
// truth: the Lambda is given the queue's own value, so the two cannot drift
// apart when the policy changes.
const DefaultMaxReceives = 5

// receiveCountAttr is the SQS message attribute that says how many times this
// message has been received — the only thing that tells the worker it is on the
// last attempt, which is why the fallback lives here and not on a Lambda
// listening to the DLQ (ADR-028).
const receiveCountAttr = "ApproximateReceiveCount"

// HandleSQS processes one batch from the queue.
//
// The event source mapping is configured with a batch of one, so "batch" is a
// single message in practice; the loop is here because the shape of the event
// allows more. Records are processed in order and the first failure returns,
// leaving that message and everything after it to be redelivered — which is the
// only way to keep a group's order without partial-batch reporting. A record
// already answered on the earlier delivery is skipped by the mark in DynamoDB,
// so redelivering the batch does not redo the work that succeeded.
func (w *Worker) HandleSQS(ctx context.Context, event events.SQSEvent) error {
	for _, record := range event.Records {
		env, err := wainbound.Unmarshal([]byte(record.Body))
		if err != nil {
			return fmt.Errorf("sqs message %s: %w", record.MessageId, err)
		}
		if err := w.Process(ctx, env, w.lastAttempt(record)); err != nil {
			return err
		}
	}
	return nil
}

// lastAttempt reports whether the redrive policy will send this message to the
// DLQ if this delivery fails.
//
// An unreadable count reads as "not the last attempt": telling someone the turn
// failed while a retry is still coming would apologise for an answer they are
// about to get.
func (w *Worker) lastAttempt(record events.SQSMessage) bool {
	raw, ok := record.Attributes[receiveCountAttr]
	if !ok {
		return false
	}
	count, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("unreadable %s=%q on message %s", receiveCountAttr, raw, record.MessageId)
		return false
	}
	return count >= w.maxReceives
}
