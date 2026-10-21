package deliveryfailure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	streamName     = "CHAT_EVENTS"
	failureSubject = "chat.message.delivery_failed"
	failureType    = "chat.message.delivery_failed.v1"
)

type failureEvent struct {
	Type           string    `json:"type"`
	SourceEventID  string    `json:"sourceEventId,omitempty"`
	SourceStream   string    `json:"sourceStream"`
	SourceSequence uint64    `json:"sourceSequence"`
	ReasonCode     string    `json:"reasonCode"`
	OccurredAt     time.Time `json:"occurredAt"`
}

type Publisher struct {
	js jetstream.JetStream
}

var _ ports.DeliveryFailurePublisher = (*Publisher)(nil)

func NewPublisher(js jetstream.JetStream) (*Publisher, error) {
	if js == nil {
		return nil, errors.New("JetStream client cannot be nil")
	}
	return &Publisher{js: js}, nil
}

func (publisher *Publisher) PublishFailure(ctx context.Context, failure ports.DeliveryFailure) error {
	if publisher == nil || publisher.js == nil {
		return errors.New("delivery failure publisher is not initialized")
	}
	if failure.StreamSequence == 0 || failure.ReasonCode == "" {
		return errors.New("invalid delivery failure metadata")
	}
	record := failureEvent{
		Type:           failureType,
		SourceEventID:  failure.EventID,
		SourceStream:   streamName,
		SourceSequence: failure.StreamSequence,
		ReasonCode:     failure.ReasonCode,
		OccurredAt:     time.Now().UTC(),
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode delivery failure: %w", err)
	}
	ack, err := publisher.js.Publish(ctx, failureSubject, data,
		jetstream.WithMsgID(fmt.Sprintf("delivery_failed:%d", failure.StreamSequence)),
		jetstream.WithExpectStream(streamName),
	)
	if err != nil {
		return fmt.Errorf("publish delivery failure: %w", err)
	}
	if ack == nil || ack.Stream != streamName || ack.Sequence == 0 {
		return errors.New("invalid delivery failure publish acknowledgement")
	}
	return nil
}
