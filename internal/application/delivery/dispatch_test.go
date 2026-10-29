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
	recipients []string
	events     []ports.RealtimeMessageCreated
	failOnCall int
	recipient  user.ID
	event      ports.RealtimeMessageCreated
	err        error
	calls      int
}

func (stub *realtimeStub) PublishMessageCreated(_ context.Context, recipient user.ID, event ports.RealtimeMessageCreated) error {
	stub.calls++
	stub.recipients = append(stub.recipients, recipient.String())
	stub.events = append(stub.events, event)
	stub.recipient = recipient
	stub.event = event
	if stub.failOnCall == 0 || stub.calls == stub.failOnCall {
		return stub.err
	}
	return nil
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

func TestDispatchMessageCreatedPublishesToBothParticipants(t *testing.T) {
	event, conversation := fixture(t)
	event.Payload.ClientMessageID = "018f92d4-2c47-7c87-b33e-d2ea331d2ff2"
	chats := &chatLookupStub{conversation: conversation}
	realtime := &realtimeStub{}
	useCase, err := NewDispatchMessageCreated(chats, realtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := useCase.Dispatch(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if realtime.calls != 2 || realtime.recipients[0] != "recipient-1" || realtime.recipients[1] != "sender-1" {
		t.Fatalf("unexpected realtime recipient: %q (calls=%d)", realtime.recipient.String(), realtime.calls)
	}
	if realtime.event.Type != ports.RealtimeMessageCreatedType ||
		realtime.event.Version != ports.RealtimeProtocolVersion ||
		realtime.event.Data.ClientMessageID != event.Payload.ClientMessageID ||
		realtime.event.EventID != event.EventID ||
		realtime.event.Data.ID != event.Payload.MessageID ||
		realtime.event.Data.Sequence != event.Payload.Sequence {
		t.Fatalf("unexpected realtime event: %+v", realtime.event)
	}
}

func TestDispatchPartialFailureRetriesSameEvent(t *testing.T) {
	event, conversation := fixture(t)
	failure := errors.New("second publication failed")
	realtime := &realtimeStub{err: failure, failOnCall: 2}
	useCase, err := NewDispatchMessageCreated(&chatLookupStub{conversation: conversation}, realtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := useCase.Dispatch(context.Background(), event); !errors.Is(err, failure) {
		t.Fatalf("expected partial failure, got %v", err)
	}
	realtime.err = nil
	if err := useCase.Dispatch(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(realtime.events) != 4 {
		t.Fatalf("expected both fanouts, got %d", len(realtime.events))
	}
	for _, published := range realtime.events {
		if published.EventID != event.EventID {
			t.Fatal("retry changed event ID")
		}
	}
}

func TestDispatchAcceptsLegacyClientKeyAndRejectsUnsafeSequence(t *testing.T) {
	event, conversation := fixture(t)
	useCase, _ := NewDispatchMessageCreated(&chatLookupStub{conversation: conversation}, &realtimeStub{})
	if err := useCase.Dispatch(context.Background(), event); err != nil {
		t.Fatalf("legacy event: %v", err)
	}
	event.Payload.Sequence = 1 << 53
	if err := useCase.Dispatch(context.Background(), event); !errors.Is(err, ports.ErrInvalidMessageCreatedEvent) {
		t.Fatalf("unsafe sequence accepted: %v", err)
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
