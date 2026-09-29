package timelinerepository

import (
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type messageContentDocument struct {
	Type  string `bson:"type"`
	Value string `bson:"value"`
}

type messageDocument struct {
	SenderID string                 `bson:"sender_id"`
	Content  messageContentDocument `bson:"content"`
}

type timelineItemDocument struct {
	ID        string          `bson:"_id"`
	ChatID    string          `bson:"chat_id"`
	Kind      string          `bson:"kind"`
	CreatedAt time.Time       `bson:"created_at"`
	UpdatedAt time.Time       `bson:"updated_at"`
	Message   messageDocument `bson:"message"`
}

func (document timelineItemDocument) toDomain() (*timeline.Item, error) {
	itemID, err := timeline.ParseID(document.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: parse item id: %v", ErrInvalidDocument, err)
	}
	chatID, err := chat.ParseID(document.ChatID)
	if err != nil {
		return nil, fmt.Errorf("%w: parse chat id: %v", ErrInvalidDocument, err)
	}
	kind, err := timeline.ParseKind(document.Kind)
	if err != nil || kind != timeline.KindMessage {
		return nil, fmt.Errorf("%w: unsupported kind %q", ErrInvalidDocument, document.Kind)
	}
	senderID, err := user.ParseID(document.Message.SenderID)
	if err != nil {
		return nil, fmt.Errorf("%w: parse sender id: %v", ErrInvalidDocument, err)
	}
	contentType, err := timeline.ParseContentType(document.Message.Content.Type)
	if err != nil {
		return nil, fmt.Errorf("%w: parse content type: %v", ErrInvalidDocument, err)
	}
	content, err := timeline.NewMessageContent(contentType, document.Message.Content.Value)
	if err != nil {
		return nil, fmt.Errorf("%w: restore content: %v", ErrInvalidDocument, err)
	}
	message, err := timeline.NewMessage(senderID, content)
	if err != nil {
		return nil, fmt.Errorf("%w: restore message: %v", ErrInvalidDocument, err)
	}
	item, err := timeline.RestoreMessage(itemID, chatID, document.CreatedAt, document.UpdatedAt, message)
	if err != nil {
		return nil, fmt.Errorf("%w: restore item: %v", ErrInvalidDocument, err)
	}
	return item, nil
}
