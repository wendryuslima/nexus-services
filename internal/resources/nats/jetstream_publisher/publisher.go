package jetstreampublisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	streamName = "CHAT_EVENTS"

	messageCreatedType = "chat.message.created.v1"

	messageCreatedSubjectPrefix = "chat.message.created"
)

type messageCreatedEnvelope struct {
	EventID       string                      `json:"eventId"`
	EventType     string                      `json:"eventType"`
	AggregateType string                      `json:"aggregateType"`
	AggregateID   string                      `json:"aggregateId"`
	OccurredAt    time.Time                   `json:"occurredAt"`
	Payload       ports.MessageCreatedPayload `json:"payload"`
}

type Publisher struct {
	js         jetstream.JetStream
	partitions uint32
}

var _ ports.EventPublisher = (*Publisher)(nil)

func NewPublisher(
	js jetstream.JetStream,
	partitions uint32,
) (*Publisher, error) {
	if js == nil {
		return nil, errors.New("JetStream client cannot be nil")
	}
	if partitions == 0 {
		return nil, errors.New("partition count must be positive")
	}

	return &Publisher{js: js, partitions: partitions}, nil
}

func subjectForChat(chatID string, partitions uint32) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(chatID))

	partition := hash.Sum32() % partitions
	return fmt.Sprintf("%s.%d", messageCreatedSubjectPrefix, partition)
}

func validateEvent(event ports.OutboxEvent) error {
	payload := event.Payload

	if strings.TrimSpace(event.ID) == "" ||
		event.EventType != messageCreatedType ||
		event.AggregateType != "CHAT" ||
		strings.TrimSpace(payload.ChatID) == "" ||
		event.AggregateID != payload.ChatID ||
		strings.TrimSpace(payload.MessageID) == "" ||
		strings.TrimSpace(payload.SenderID) == "" ||
		payload.Sequence <= 0 ||
		payload.CreatedAt.IsZero() ||
		payload.Content.Type != "TEXT" ||
		strings.TrimSpace(payload.Content.Value) == "" {
		return ports.ErrInvalidOutboxEvent
	}

	return nil
}

func (publisher *Publisher) Publish(
	ctx context.Context,
	event ports.OutboxEvent,
) (ports.PublishReceipt, error) {
	if publisher == nil || publisher.js == nil {
		return ports.PublishReceipt{}, errors.New("publisher is not initialized")
	}
	if err := validateEvent(event); err != nil {
		return ports.PublishReceipt{}, err
	}

	envelope := messageCreatedEnvelope{
		EventID:       event.ID,
		EventType:     event.EventType,
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		OccurredAt:    event.Payload.CreatedAt.UTC(),
		Payload:       event.Payload,
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return ports.PublishReceipt{}, fmt.Errorf("encode outbox event: %w", err)
	}

	subject := subjectForChat(event.Payload.ChatID, publisher.partitions)

	ack, err := publisher.js.Publish(
		ctx,
		subject,
		data,

		jetstream.WithMsgID(event.ID),

		jetstream.WithExpectStream(streamName),
	)
	if err != nil {
		return ports.PublishReceipt{}, fmt.Errorf("publish outbox event: %w", err)
	}
	if ack == nil || ack.Stream != streamName || ack.Sequence == 0 {
		return ports.PublishReceipt{}, errors.New("invalid JetStream publish acknowledgement")
	}

	return ports.PublishReceipt{
		Stream:   ack.Stream,
		Sequence: ack.Sequence,
	}, nil
}
