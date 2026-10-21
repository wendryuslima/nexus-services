package realtimepublisher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type connectionStub struct {
	subject string
	data    []byte
	flushed bool
	order   []string
}

func (stub *connectionStub) Publish(subject string, data []byte) error {
	stub.subject = subject
	stub.data = data
	stub.order = append(stub.order, "publish")
	return nil
}

func (stub *connectionStub) FlushWithContext(context.Context) error {
	stub.flushed = true
	stub.order = append(stub.order, "flush")
	return nil
}

func TestPublishMessageCreatedUsesRecipientSubject(t *testing.T) {
	connection := &connectionStub{}
	publisher, err := NewPublisher(connection)
	if err != nil {
		t.Fatal(err)
	}
	recipientID, err := user.ParseID("recipient-1")
	if err != nil {
		t.Fatal(err)
	}
	event := ports.RealtimeMessageCreated{
		Type:       ports.RealtimeMessageCreatedType,
		EventID:    "event-1",
		OccurredAt: time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC),
		Data:       ports.RealtimeMessageData{ID: "message-1"},
	}
	if err := publisher.PublishMessageCreated(context.Background(), recipientID, event); err != nil {
		t.Fatal(err)
	}
	if connection.subject != "realtime.user.recipient-1" || !connection.flushed ||
		len(connection.order) != 2 || connection.order[0] != "publish" || connection.order[1] != "flush" {
		t.Fatalf("wrong Core NATS operation: subject=%q order=%v", connection.subject, connection.order)
	}
	var stored ports.RealtimeMessageCreated
	if err := json.Unmarshal(connection.data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.EventID != event.EventID || stored.Data.ID != event.Data.ID {
		t.Fatalf("wrong public event: %+v", stored)
	}
}

func TestPublishMessageCreatedRejectsSubjectInjection(t *testing.T) {
	connection := &connectionStub{}
	publisher, err := NewPublisher(connection)
	if err != nil {
		t.Fatal(err)
	}
	recipientID, err := user.ParseID("recipient.other")
	if err != nil {
		t.Fatal(err)
	}
	err = publisher.PublishMessageCreated(context.Background(), recipientID, ports.RealtimeMessageCreated{
		Type: ports.RealtimeMessageCreatedType, EventID: "event-1",
	})
	if !errors.Is(err, ports.ErrInvalidMessageCreatedEvent) || connection.subject != "" {
		t.Fatalf("expected invalid recipient without NATS publication, got %v", err)
	}
}
