package timeline

import (
	"strings"
	"time"
	"unicode/utf8"

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
	ContentTypeText  ContentType = "TEXT"
	ContentTypeImage ContentType = "IMAGE"
)

func ParseContentType(rawType string) (ContentType, error) {
	contentType := ContentType(strings.ToUpper(strings.TrimSpace(rawType)))
	if contentType != ContentTypeText && contentType != ContentTypeImage {
		return "", ErrInvalidContentType
	}

	return contentType, nil
}

type MessageContent struct {
	contentType ContentType
	value       string
	imageID     string
	caption     string
}

func NewImageContent(imageID, caption string) (MessageContent, error) {
	if strings.TrimSpace(imageID) == "" || strings.TrimSpace(imageID) != imageID || len(imageID) > 128 || !utf8.ValidString(caption) || utf8.RuneCountInString(caption) > 4000 {
		return MessageContent{}, ErrInvalidContent
	}
	return MessageContent{contentType: ContentTypeImage, imageID: imageID, caption: caption}, nil
}

func RestoreContent(kind ContentType, value, imageID, caption string) (MessageContent, error) {
	switch kind {
	case ContentTypeText:
		if imageID != "" || caption != "" {
			return MessageContent{}, ErrInvalidContent
		}
		return NewMessageContent(kind, value)
	case ContentTypeImage:
		if value != "" {
			return MessageContent{}, ErrInvalidContent
		}
		return NewImageContent(imageID, caption)
	default:
		return MessageContent{}, ErrInvalidContentType
	}
}

func (content MessageContent) ImageID() string { return content.imageID }
func (content MessageContent) Caption() string { return content.caption }
func (content MessageContent) Valid() bool {
	_, err := RestoreContent(content.contentType, content.value, content.imageID, content.caption)
	return err == nil
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

	if !content.Valid() {
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

	if !message.Content().Valid() {
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
