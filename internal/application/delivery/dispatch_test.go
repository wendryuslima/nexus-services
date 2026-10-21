package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type chatLookupStub struct {
	conversation *chat.Chat
	err          error
	calls        int
}

func (stub *chatLookupStub) FindByID(context.Context, chat.ID) (*chat.Chat, error) {
	stub.calls++
	return stub.conversation, stub.err
}

type realtimeStub struct {
	recipient user.ID
	event     ports.RealtimeMessageCreated
	err       error
	calls     int
}

func (stub *realtimeStub) PublishMessageCreated(_ context.Context, recipient user.ID, event ports.RealtimeMessageCreated) error {
	stub.calls++
	stub.recipient = recipient
	stub.event = event
	return stub.err
}

func fixture(t *testing.T) (ports.MessageCreatedEvent, *chat.Chat) {
	t.Helper()
	chatID, err := chat.ParseID("chat-1")
	if err != nil {
		t.Fatal(err)
	}
	senderID, err := user.ParseID("sender-1")
	if err != nil {
		t.Fatal(err)
	}
	recipientID, err := user.ParseID("recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	conversation, err := chat.NewDirect(chatID, senderID, recipientID, now)
	if err != nil {
		t.Fatal(err)
	}
	event := ports.MessageCreatedEvent{
		EventID:       "event-1",
		EventType:     ports.MessageCreatedEventTypeV1,
		AggregateType: "CHAT",
		AggregateID:   chatID.String(),
		OccurredAt:    now,
		Payload: ports.MessageCreatedPayload{
			MessageID: "message-1",
			ChatID:    chatID.String(),
			Sequence:  7,
			SenderID:  senderID.String(),
			Content:   ports.OutboxContent{Type: "TEXT", Value: "Olá"},
			CreatedAt: now,
		},
	}
	return event, conversation
}

func TestDispatchMessageCreatedPublishesToOtherParticipant(t *testing.T) {
	event, conversation := fixture(t)
	chats := &chatLookupStub{conversation: conversation}
	realtime := &realtimeStub{}
	useCase, err := NewDispatchMessageCreated(chats, realtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := useCase.Dispatch(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if realtime.calls != 1 || realtime.recipient.String() != "recipient-1" {
		t.Fatalf("unexpected realtime recipient: %q (calls=%d)", realtime.recipient.String(), realtime.calls)
	}
	if realtime.event.Type != ports.RealtimeMessageCreatedType ||
		realtime.event.EventID != event.EventID ||
		realtime.event.Data.ID != event.Payload.MessageID ||
		realtime.event.Data.Sequence != event.Payload.Sequence {
		t.Fatalf("unexpected realtime event: %+v", realtime.event)
	}
}

func TestDispatchMessageCreatedRejectsInvalidEnvelope(t *testing.T) {
	event, conversation := fixture(t)
	event.EventType = "chat.message.created.v2"
	chats := &chatLookupStub{conversation: conversation}
	realtime := &realtimeStub{}
	useCase, err := NewDispatchMessageCreated(chats, realtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := useCase.Dispatch(context.Background(), event); !errors.Is(err, ports.ErrInvalidMessageCreatedEvent) {
		t.Fatalf("expected permanent invalid-event error, got %v", err)
	}
	if chats.calls != 0 || realtime.calls != 0 {
		t.Fatal("invalid event reached external dependencies")
	}
}

func TestDispatchMessageCreatedPropagatesRealtimeFailure(t *testing.T) {
	event, conversation := fixture(t)
	realtimeErr := errors.New("NATS unavailable")
	realtime := &realtimeStub{err: realtimeErr}
	useCase, err := NewDispatchMessageCreated(&chatLookupStub{conversation: conversation}, realtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := useCase.Dispatch(context.Background(), event); !errors.Is(err, realtimeErr) {
		t.Fatalf("expected retryable NATS error, got %v", err)
	}
}
