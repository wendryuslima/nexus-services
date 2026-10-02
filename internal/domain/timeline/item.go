package timeline

import (
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type ID struct {
	value string
}

func ParseID(rawID string) (ID, error) {
	normalizedID := strings.TrimSpace(rawID)
	if normalizedID == "" {
		return ID{}, ErrInvalidID
	}

	return ID{value: normalizedID}, nil
}

func (id ID) String() string {
	return id.value
}

type Kind string

const (
	KindMessage Kind = "MESSAGE"
)

func ParseKind(rawKind string) (Kind, error) {
	kind := Kind(strings.ToUpper(strings.TrimSpace(rawKind)))
	if kind != KindMessage {
		return "", ErrInvalidKind
	}

	return kind, nil
}

type ContentType string

const (
	ContentTypeText ContentType = "TEXT"
)

func ParseContentType(rawType string) (ContentType, error) {
	contentType := ContentType(strings.ToUpper(strings.TrimSpace(rawType)))
	if contentType != ContentTypeText {
		return "", ErrInvalidContentType
	}

	return contentType, nil
}

type MessageContent struct {
	contentType ContentType
	value       string
}

func NewMessageContent(
	contentType ContentType,
	value string,
) (MessageContent, error) {
	if contentType != ContentTypeText {
		return MessageContent{}, ErrInvalidContentType
	}

	if strings.TrimSpace(value) == "" {
		return MessageContent{}, ErrInvalidContent
	}

	return MessageContent{
		contentType: contentType,
		value:       value,
	}, nil
}

func (content MessageContent) Type() ContentType {
	return content.contentType
}

func (content MessageContent) Value() string {
	return content.value
}

type Message struct {
	clientMessageID ClientMessageID
	senderID        user.ID
	content         MessageContent
}

func NewMessage(
	clientMessageID ClientMessageID,
	senderID user.ID,
	content MessageContent,
) (Message, error) {
	if clientMessageID.String() == "" {
		return Message{}, ErrInvalidClientMessageID
	}

	if senderID.String() == "" {
		return Message{}, ErrInvalidSenderID
	}

	if content.Type() != ContentTypeText ||
		strings.TrimSpace(content.Value()) == "" {
		return Message{}, ErrInvalidContent
	}

	return Message{
		clientMessageID: clientMessageID,
		senderID:        senderID,
		content:         content,
	}, nil
}

func (message Message) ClientMessageID() ClientMessageID {
	return message.clientMessageID
}

func (message Message) SenderID() user.ID {
	return message.senderID
}

func (message Message) Content() MessageContent {
	return message.content
}

type Item struct {
	id        ID
	chatID    chat.ID
	sequence  Sequence
	kind      Kind
	createdAt time.Time
	updatedAt time.Time
	message   Message
}

func RestoreMessage(
	id ID,
	chatID chat.ID,
	sequence Sequence,
	createdAt time.Time,
	updatedAt time.Time,
	message Message,
) (*Item, error) {
	if id.String() == "" {
		return nil, ErrInvalidID
	}

	if chatID.String() == "" {
		return nil, ErrInvalidChatID
	}

	if sequence.Int64() <= 0 {
		return nil, ErrInvalidSequence
	}

	if createdAt.IsZero() {
		return nil, ErrInvalidCreatedAt
	}

	if updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return nil, ErrInvalidUpdatedAt
	}

	if message.ClientMessageID().String() == "" {
		return nil, ErrInvalidClientMessageID
	}

	if message.SenderID().String() == "" {
		return nil, ErrInvalidSenderID
	}

	if message.Content().Type() != ContentTypeText ||
		strings.TrimSpace(message.Content().Value()) == "" {
		return nil, ErrInvalidContent
	}

	return &Item{
		id:        id,
		chatID:    chatID,
		sequence:  sequence,
		kind:      KindMessage,
		createdAt: createdAt.UTC(),
		updatedAt: updatedAt.UTC(),
		message:   message,
	}, nil
}

func (item *Item) ID() ID {
	return item.id
}

func (item *Item) ChatID() chat.ID {
	return item.chatID
}

func (item *Item) Sequence() Sequence {
	return item.sequence
}

func (item *Item) Kind() Kind {
	return item.kind
}

func (item *Item) CreatedAt() time.Time {
	return item.createdAt
}

func (item *Item) UpdatedAt() time.Time {
	return item.updatedAt
}

func (item *Item) Message() Message {
	return item.message
}
