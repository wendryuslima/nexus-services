package ports

import (
	"context"
	"errors"
	"time"
)

const MessageCreatedEventTypeV1 = "chat.message.created.v1"

var ErrInvalidMessageCreatedEvent = errors.New("invalid message created event")

type MessageCreatedEvent struct {
	EventID       string                `json:"eventId"`
	EventType     string                `json:"eventType"`
	AggregateType string                `json:"aggregateType"`
	AggregateID   string                `json:"aggregateId"`
	OccurredAt    time.Time             `json:"occurredAt"`
	Payload       MessageCreatedPayload `json:"payload"`
}

type MessageDeliveryHandler interface {
	Dispatch(ctx context.Context, event MessageCreatedEvent) error
}
