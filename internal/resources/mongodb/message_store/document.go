package messagestore

import (
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	aggregateTypeChat = "CHAT"

	messageCreatedEventType = "chat.message.created.v1"

	outboxStatusPending = "PENDING"
)

type messageContentDocument struct {
	Type  string `bson:"type"`
	Value string `bson:"value"`
}

type messageDocument struct {
	ClientMessageID string                 `bson:"client_message_id"`
	SenderID        string                 `bson:"sender_id"`
	Content         messageContentDocument `bson:"content"`
}

type timelineItemDocument struct {
	ID        string          `bson:"_id"`
	ChatID    string          `bson:"chat_id"`
	Sequence  int64           `bson:"sequence"`
	Kind      string          `bson:"kind"`
	CreatedAt time.Time       `bson:"created_at"`
	UpdatedAt time.Time       `bson:"updated_at"`
	Message   messageDocument `bson:"message"`
}

type eventContentDocument struct {
	Type  string `bson:"type" json:"type"`
	Value string `bson:"value" json:"value"`
}

type messageCreatedPayloadDocument struct {
	MessageID string               `bson:"messageId" json:"messageId"`
	ChatID    string               `bson:"chatId" json:"chatId"`
	Sequence  int64                `bson:"sequence" json:"sequence"`
	SenderID  string               `bson:"senderId" json:"senderId"`
	Content   eventContentDocument `bson:"content" json:"content"`
	CreatedAt time.Time            `bson:"createdAt" json:"createdAt"`
}

type outboxEventDocument struct {
	ID            string                        `bson:"_id"`
	AggregateType string                        `bson:"aggregate_type"`
	AggregateID   string                        `bson:"aggregate_id"`
	EventType     string                        `bson:"event_type"`
	Payload       messageCreatedPayloadDocument `bson:"payload"`

	Status        string     `bson:"status"`
	Attempts      int64      `bson:"attempts"`
	NextAttemptAt time.Time  `bson:"next_attempt_at"`
	LockedBy      *string    `bson:"locked_by"`
	LockedUntil   *time.Time `bson:"locked_until"`
	PublishedAt   *time.Time `bson:"published_at"`
	LastError     *string    `bson:"last_error"`
	CreatedAt     time.Time  `bson:"created_at"`
}

func newTimelineItemDocument(
	params ports.CreateMessageParams,
	sequence timeline.Sequence,
) timelineItemDocument {
	message := params.Message
	content := message.Content()
	createdAt := params.CreatedAt.UTC()

	return timelineItemDocument{
		ID:        params.MessageID.String(),
		ChatID:    params.ChatID.String(),
		Sequence:  sequence.Int64(),
		Kind:      string(timeline.KindMessage),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		Message: messageDocument{
			ClientMessageID: message.ClientMessageID().String(),
			SenderID:        message.SenderID().String(),
			Content: messageContentDocument{
				Type:  string(content.Type()),
				Value: content.Value(),
			},
		},
	}
}

func newOutboxEventDocument(
	params ports.CreateMessageParams,
	sequence timeline.Sequence,
) outboxEventDocument {
	message := params.Message
	content := message.Content()
	createdAt := params.CreatedAt.UTC()

	return outboxEventDocument{
		ID:            params.EventID,
		AggregateType: aggregateTypeChat,
		AggregateID:   params.ChatID.String(),
		EventType:     messageCreatedEventType,
		Payload: messageCreatedPayloadDocument{
			MessageID: params.MessageID.String(),
			ChatID:    params.ChatID.String(),
			Sequence:  sequence.Int64(),
			SenderID:  message.SenderID().String(),
			Content: eventContentDocument{
				Type:  string(content.Type()),
				Value: content.Value(),
			},
			CreatedAt: createdAt,
		},
		Status:        outboxStatusPending,
		Attempts:      0,
		NextAttemptAt: createdAt,
		LockedBy:      nil,
		LockedUntil:   nil,
		PublishedAt:   nil,
		LastError:     nil,
		CreatedAt:     createdAt,
	}
}
