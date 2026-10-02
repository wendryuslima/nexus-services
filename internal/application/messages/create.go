package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	maxTextMessageCharacters = 4000
)

type DeliveryStatus string

const (
	DeliveryStatusPersisted DeliveryStatus = "PERSISTED"
)

type CreateContentInput struct {
	Type  string
	Value string
}

type CreateInput struct {
	CurrentUserID   string
	ChatID          string
	ClientMessageID string
	Content         CreateContentInput
}

type CreatedContent struct {
	Type  string
	Value string
}

type CreateOutput struct {
	ID              string
	ClientMessageID string
	ChatID          string
	Sequence        int64
	SenderID        string
	Content         CreatedContent
	CreatedAt       time.Time
	DeliveryStatus  DeliveryStatus
	Created         bool
}

type CreateDependencies struct {
	ChatRepository ports.ChatRepository
	MessageStore   ports.MessageStore
	IDGenerator    ports.IDGenerator
	Clock          ports.Clock
}

type CreateUseCase struct {
	chatRepository ports.ChatRepository
	messageStore   ports.MessageStore
	idGenerator    ports.IDGenerator
	clock          ports.Clock
}

func NewCreateUseCase(
	dependencies CreateDependencies,
) (*CreateUseCase, error) {
	if dependencies.ChatRepository == nil {
		return nil, fmt.Errorf(
			"%w: chat repository",
			ErrNilDependency,
		)
	}

	if dependencies.MessageStore == nil {
		return nil, fmt.Errorf(
			"%w: message store",
			ErrNilDependency,
		)
	}

	if dependencies.IDGenerator == nil {
		return nil, fmt.Errorf(
			"%w: id generator",
			ErrNilDependency,
		)
	}

	if dependencies.Clock == nil {
		return nil, fmt.Errorf(
			"%w: clock",
			ErrNilDependency,
		)
	}

	return &CreateUseCase{
		chatRepository: dependencies.ChatRepository,
		messageStore:   dependencies.MessageStore,
		idGenerator:    dependencies.IDGenerator,
		clock:          dependencies.Clock,
	}, nil
}

func (useCase *CreateUseCase) Execute(
	ctx context.Context,
	input CreateInput,
) (CreateOutput, error) {
	currentUserID, err := user.ParseID(input.CurrentUserID)
	if err != nil {
		return CreateOutput{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidCurrentUserID,
			err,
		)
	}

	chatID, err := chat.ParseID(input.ChatID)
	if err != nil {
		return CreateOutput{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidChatID,
			err,
		)
	}

	clientMessageID, err := timeline.ParseClientMessageID(
		input.ClientMessageID,
	)
	if err != nil {
		return CreateOutput{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidClientMessageID,
			err,
		)
	}

	content, err := parseContent(input.Content)
	if err != nil {
		return CreateOutput{}, err
	}

	conversation, err := useCase.chatRepository.FindByID(ctx, chatID)
	if errors.Is(err, ports.ErrChatNotFound) {
		return CreateOutput{}, ErrChatNotFound
	}
	if err != nil {
		return CreateOutput{}, fmt.Errorf("find chat: %w", err)
	}
	if conversation == nil {
		return CreateOutput{}, errors.New(
			"chat repository returned a nil chat",
		)
	}

	if !conversation.HasParticipant(currentUserID) {
		return CreateOutput{}, ErrChatNotFound
	}

	message, err := timeline.NewMessage(
		clientMessageID,
		currentUserID,
		content,
	)
	if err != nil {
		return CreateOutput{}, fmt.Errorf(
			"create message value: %w",
			err,
		)
	}

	messageID, err := useCase.generateMessageID(ctx)
	if err != nil {
		return CreateOutput{}, err
	}

	eventID, err := useCase.generateEventID(ctx)
	if err != nil {
		return CreateOutput{}, err
	}

	createdAt := useCase.clock.Now().UTC()
	if createdAt.IsZero() {
		return CreateOutput{}, errors.New(
			"clock returned a zero creation time",
		)
	}

	result, err := useCase.messageStore.CreateWithOutbox(
		ctx,
		ports.CreateMessageParams{
			MessageID: messageID,
			EventID:   eventID,
			ChatID:    chatID,
			Message:   message,
			CreatedAt: createdAt,
		},
	)
	if errors.Is(err, ports.ErrMessageIdempotencyConflict) {
		return CreateOutput{}, ErrIdempotencyConflict
	}
	if errors.Is(err, ports.ErrChatNotFound) {
		return CreateOutput{}, ErrChatNotFound
	}
	if err != nil {
		return CreateOutput{}, fmt.Errorf(
			"create message with outbox: %w",
			err,
		)
	}

	if err := validateStoredItem(
		result.Item,
		chatID,
		message,
	); err != nil {
		return CreateOutput{}, err
	}

	return mapCreateOutput(result), nil
}

func parseContent(
	input CreateContentInput,
) (timeline.MessageContent, error) {
	contentType, err := timeline.ParseContentType(input.Type)
	if err != nil {
		return timeline.MessageContent{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidContentType,
			err,
		)
	}

	if !utf8.ValidString(input.Value) {
		return timeline.MessageContent{}, ErrInvalidContent
	}

	if utf8.RuneCountInString(input.Value) > maxTextMessageCharacters {
		return timeline.MessageContent{}, ErrContentTooLong
	}

	content, err := timeline.NewMessageContent(
		contentType,
		input.Value,
	)
	if err != nil {
		return timeline.MessageContent{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidContent,
			err,
		)
	}

	return content, nil
}

func (useCase *CreateUseCase) generateMessageID(
	ctx context.Context,
) (timeline.ID, error) {
	rawID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return timeline.ID{}, fmt.Errorf(
			"generate message id: %w",
			err,
		)
	}

	messageID, err := timeline.ParseID(rawID)
	if err != nil {
		return timeline.ID{}, fmt.Errorf(
			"parse generated message id: %w",
			err,
		)
	}

	return messageID, nil
}

func (useCase *CreateUseCase) generateEventID(
	ctx context.Context,
) (string, error) {
	eventID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return "", fmt.Errorf(
			"generate outbox event id: %w",
			err,
		)
	}

	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return "", errors.New(
			"id generator returned an empty outbox event id",
		)
	}

	return eventID, nil
}

func validateStoredItem(
	item *timeline.Item,
	expectedChatID chat.ID,
	expectedMessage timeline.Message,
) error {
	if item == nil {
		return errors.New("message store returned a nil item")
	}

	if item.ChatID().String() != expectedChatID.String() {
		return errors.New(
			"message store returned an item from another chat",
		)
	}

	if item.Sequence().Int64() <= 0 {
		return errors.New(
			"message store returned an invalid sequence",
		)
	}

	storedMessage := item.Message()

	if storedMessage.ClientMessageID().String() !=
		expectedMessage.ClientMessageID().String() {
		return errors.New(
			"message store returned another client message id",
		)
	}

	if storedMessage.SenderID().String() !=
		expectedMessage.SenderID().String() {
		return errors.New(
			"message store returned another sender",
		)
	}

	storedContent := storedMessage.Content()
	expectedContent := expectedMessage.Content()

	if storedContent.Type() != expectedContent.Type() ||
		storedContent.Value() != expectedContent.Value() {
		return errors.New(
			"message store returned different message content",
		)
	}

	return nil
}

func mapCreateOutput(
	result ports.CreateMessageResult,
) CreateOutput {
	item := result.Item
	message := item.Message()
	content := message.Content()

	return CreateOutput{
		ID:              item.ID().String(),
		ClientMessageID: message.ClientMessageID().String(),
		ChatID:          item.ChatID().String(),
		Sequence:        item.Sequence().Int64(),
		SenderID:        message.SenderID().String(),
		Content: CreatedContent{
			Type:  string(content.Type()),
			Value: content.Value(),
		},
		CreatedAt:      item.CreatedAt(),
		DeliveryStatus: DeliveryStatusPersisted,
		Created:        result.Created,
	}
}
