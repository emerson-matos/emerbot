package wainbound

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// SQSAPI is the subset of *sqs.Client this repo issues — one call. It exists for
// the same reason dynamostore.API does: the publisher is exercised against a
// fake instead of a live queue (ADR-014).
type SQSAPI interface {
	SendMessage(ctx context.Context, in *sqs.SendMessageInput, opts ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

var _ SQSAPI = (*sqs.Client)(nil)

// SQSPublisher puts envelopes on the FIFO queue the worker reads.
type SQSPublisher struct {
	client   SQSAPI
	queueURL string
}

// NewSQSPublisher builds a publisher from the ambient AWS config. A non-empty
// endpoint overrides the resolved one, which is how a local queue is addressed
// under podman compose.
func NewSQSPublisher(ctx context.Context, queueURL, endpoint string) (*SQSPublisher, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if endpoint != "" {
		opts = append(opts, awsconfig.WithBaseEndpoint(endpoint))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("wainbound: load aws config: %w", err)
	}
	client := sqs.NewFromConfig(cfg, func(o *sqs.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return NewSQSPublisherWithClient(client, queueURL), nil
}

// NewSQSPublisherWithClient builds a publisher over any SQSAPI.
func NewSQSPublisherWithClient(client SQSAPI, queueURL string) *SQSPublisher {
	return &SQSPublisher{client: client, queueURL: queueURL}
}

// Publish enqueues one envelope.
//
// MessageGroupId is the sender's phone, which is the whole reason this is a FIFO
// queue and not an async invoke: the agent reads the conversation history as
// input, so two messages from the same phone processed in parallel are not two
// independent runs — the second can read a past the first has not written yet
// (ADR-028). The group serializes one conversation and leaves different
// conversations running side by side.
//
// MessageDeduplicationId is Meta's message id, the identifier Meta repeats when
// it retries, so a transport retry is dropped before it costs an invocation.
// That window is short and belongs to SQS; the domain's own "we already answered
// this" lives in DynamoDB, on the worker's side (ADR-029).
func (p *SQSPublisher) Publish(ctx context.Context, env Envelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	body, err := env.Marshal()
	if err != nil {
		return err
	}
	_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(p.queueURL),
		MessageBody:            aws.String(string(body)),
		MessageGroupId:         aws.String(env.Message.UserID),
		MessageDeduplicationId: aws.String(env.Message.MessageID),
	})
	if err != nil {
		return fmt.Errorf("wainbound: send message %s: %w", env.Message.MessageID, err)
	}
	return nil
}
