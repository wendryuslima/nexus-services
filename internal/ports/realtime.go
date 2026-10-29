package ports

import (
	"context"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

const (
	RealtimeProtocolVersion = 1

	RealtimeMessageCreatedType = "message.created"
)

type RealtimeMessageData struct {
	ID string `json:"id"`

	ClientMessageID string `json:"clientMessageId,omitempty"`

	ChatID string `json:"chatId"`

	Sequence int64 `json:"sequence"`

	SenderID string `json:"senderId"`

	Content OutboxContent `json:"content"`

	CreatedAt time.Time `json:"createdAt"`
}

type RealtimeMessageCreated struct {
	Version int `json:"version"`

	Type string `json:"type"`

	EventID string `json:"eventId"`

	OccurredAt time.Time `json:"occurredAt"`

	Data RealtimeMessageData `json:"data"`
}

type RealtimePublisher interface {
	PublishMessageCreated(
		ctx context.Context,
		recipientID user.ID,
		event RealtimeMessageCreated,
	) error
}
