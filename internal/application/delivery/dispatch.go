package delivery

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type chatLookup interface {
	FindByID(ctx context.Context, chatID chat.ID) (*chat.Chat, error)
}

type DispatchMessageCreated struct {
	chats    chatLookup
	realtime ports.RealtimePublisher
}

var _ ports.MessageDeliveryHandler = (*DispatchMessageCreated)(nil)

func NewDispatchMessageCreated(chats chatLookup, realtime ports.RealtimePublisher) (*DispatchMessageCreated, error) {
	if chats == nil || realtime == nil {
		return nil, errors.New("delivery dependencies cannot be nil")
	}
	return &DispatchMessageCreated{chats: chats, realtime: realtime}, nil
}

func validateMessageCreated(event ports.MessageCreatedEvent) error {
	payload := event.Payload
	if strings.TrimSpace(event.EventID) == "" ||
		event.EventType != ports.MessageCreatedEventTypeV1 ||
		event.AggregateType != "CHAT" ||
		strings.TrimSpace(event.AggregateID) == "" ||
		event.AggregateID != payload.ChatID ||
		event.OccurredAt.IsZero() ||
		strings.TrimSpace(payload.MessageID) == "" ||
		strings.TrimSpace(payload.SenderID) == "" ||
		payload.Sequence <= 0 ||
		payload.Sequence > timeline.MaxSequence ||
		payload.CreatedAt.IsZero() ||
		payload.Content.Type != "TEXT" ||
		strings.TrimSpace(payload.Content.Value) == "" {
		return ports.ErrInvalidMessageCreatedEvent
	}
	if payload.ClientMessageID != "" {
		if _, err := timeline.ParseClientMessageID(payload.ClientMessageID); err != nil {
			return ports.ErrInvalidMessageCreatedEvent
		}
	}
	return nil
}

func (useCase *DispatchMessageCreated) Dispatch(ctx context.Context, event ports.MessageCreatedEvent) error {
	if useCase == nil {
		return errors.New("delivery use case is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateMessageCreated(event); err != nil {
		return err
	}

	chatID, err := chat.ParseID(event.Payload.ChatID)
	if err != nil {
		return ports.ErrInvalidMessageCreatedEvent
	}
	senderID, err := user.ParseID(event.Payload.SenderID)
	if err != nil {
		return ports.ErrInvalidMessageCreatedEvent
	}

	conversation, err := useCase.chats.FindByID(ctx, chatID)
	if errors.Is(err, ports.ErrChatNotFound) {
		return fmt.Errorf("%w: chat_not_found", ports.ErrInvalidMessageCreatedEvent)
	}
	if err != nil {
		return fmt.Errorf("find delivery chat: %w", err)
	}
	if conversation == nil {
		return errors.New("chat repository returned nil conversation")
	}

	recipientID, err := conversation.RelatedUserID(senderID)
	if errors.Is(err, chat.ErrUserNotParticipant) {
		return fmt.Errorf("%w: sender_not_participant", ports.ErrInvalidMessageCreatedEvent)
	}
	if err != nil {
		return fmt.Errorf("resolve delivery recipient: %w", err)
	}

	publicEvent := ports.RealtimeMessageCreated{
		Version:    ports.RealtimeProtocolVersion,
		Type:       ports.RealtimeMessageCreatedType,
		EventID:    event.EventID,
		OccurredAt: event.OccurredAt.UTC(),
		Data: ports.RealtimeMessageData{
			ID:              event.Payload.MessageID,
			ClientMessageID: event.Payload.ClientMessageID,
			ChatID:          event.Payload.ChatID,
			Sequence:        event.Payload.Sequence,
			SenderID:        event.Payload.SenderID,
			Content:         event.Payload.Content,
			CreatedAt:       event.Payload.CreatedAt.UTC(),
		},
	}
	if err := useCase.realtime.PublishMessageCreated(ctx, recipientID, publicEvent); err != nil {
		return fmt.Errorf("publish recipient notification: %w", err)
	}
	if err := useCase.realtime.PublishMessageCreated(ctx, senderID, publicEvent); err != nil {
		return fmt.Errorf("publish sender notification: %w", err)
	}
	return nil
}
