package ports

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidOutboxEvent = errors.New("invalid outbox event")

type OutboxContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type MessageCreatedPayload struct {
	MessageID       string        `json:"messageId"`
	ClientMessageID string        `json:"clientMessageId,omitempty"`
	ChatID          string        `json:"chatId"`
	Sequence        int64         `json:"sequence"`
	SenderID        string        `json:"senderId"`
	Content         OutboxContent `json:"content"`
	CreatedAt       time.Time     `json:"createdAt"`
}

type OutboxEvent struct {
	ID            string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       MessageCreatedPayload
	Attempts      int64
	LeaseID       string
}

type PublishReceipt struct {
	Stream   string
	Sequence uint64
}

type OutboxStore interface {
	ClaimNext(ctx context.Context, workerID string, leaseID string, now time.Time, leaseDuration time.Duration) (*OutboxEvent, error)
	MarkPublished(ctx context.Context, eventID string, leaseID string, receipt PublishReceipt, publishedAt time.Time) (bool, error)
	ScheduleRetry(ctx context.Context, eventID string, leaseID string, nextAttemptAt time.Time, sanitizedError string) (bool, error)
	MarkFailed(ctx context.Context, eventID string, leaseID string, failedAt time.Time, reasonCode string) (bool, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, event OutboxEvent) (PublishReceipt, error)
}
