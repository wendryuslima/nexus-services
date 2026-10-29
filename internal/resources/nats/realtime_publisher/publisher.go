package realtimepublisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const subjectPrefix = "realtime.user."

type Connection interface {
	Publish(subject string, data []byte) error
	FlushWithContext(ctx context.Context) error
}

type Publisher struct {
	connection Connection
}

var _ ports.RealtimePublisher = (*Publisher)(nil)

func NewPublisher(connection Connection) (*Publisher, error) {
	if connection == nil {
		return nil, errors.New("NATS connection cannot be nil")
	}
	return &Publisher{connection: connection}, nil
}

func (publisher *Publisher) PublishMessageCreated(ctx context.Context, recipientID user.ID, event ports.RealtimeMessageCreated) error {
	if publisher == nil || publisher.connection == nil {
		return errors.New("realtime publisher is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := recipientID.String()
	if !ports.ValidRealtimeUserID(id) || ports.ValidateRealtimeMessage(event) != nil {
		return ports.ErrInvalidMessageCreatedEvent
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode realtime event: %w", err)
	}
	if err := publisher.connection.Publish(subjectPrefix+id, data); err != nil {
		return fmt.Errorf("publish realtime event: %w", err)
	}
	if err := publisher.connection.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("flush realtime event: %w", err)
	}
	return nil
}
