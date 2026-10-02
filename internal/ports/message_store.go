package ports

import (
	"context"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
)

type CreateMessageParams struct {
	MessageID timeline.ID
	EventID   string
	ChatID    chat.ID
	Message   timeline.Message
	CreatedAt time.Time
}

type CreateMessageResult struct {
	Item    *timeline.Item
	Created bool
}

type MessageStore interface {
	CreateWithOutbox(ctx context.Context, params CreateMessageParams) (CreateMessageResult, error)
}
