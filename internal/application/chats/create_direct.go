package chats

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type CreateDirectInput struct {
	CurrentUserID string
	RelatedUserID string
}

type CreateDirectOutput struct {
	ID            string
	RelatedUser   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	SummarySortAt time.Time
	Created       bool
}

type CreateDirectDependencies struct {
	ChatRepository ports.ChatRepository
	UserRepository ports.UserRepository
	IDGenerator    ports.IDGenerator
	Clock          ports.Clock
}

type CreateDirectUseCase struct {
	chatRepository ports.ChatRepository
	userRepository ports.UserRepository
	idGenerator    ports.IDGenerator
	clock          ports.Clock
}

func NewCreateDirectUseCase(dependencies CreateDirectDependencies) (*CreateDirectUseCase, error) {
	if dependencies.ChatRepository == nil {
		return nil, fmt.Errorf("%w: chat repository", ErrNilDependency)
	}

	if dependencies.UserRepository == nil {
		return nil, fmt.Errorf(
			"%w: user repository",
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

	return &CreateDirectUseCase{
		chatRepository: dependencies.ChatRepository,
		userRepository: dependencies.UserRepository,
		idGenerator:    dependencies.IDGenerator,
		clock:          dependencies.Clock,
	}, nil
}

func (useCase *CreateDirectUseCase) Execute(ctx context.Context, input CreateDirectInput) (CreateDirectOutput, error) {
	currentUserID, err := user.ParseID(input.CurrentUserID)
	if err != nil {
		return CreateDirectOutput{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidCurrentUserID,
			err,
		)
	}

	relatedUserID, err := user.ParseID(input.RelatedUserID)
	if err != nil {
		return CreateDirectOutput{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidRelatedUserID,
			err,
		)
	}

	if currentUserID.String() == relatedUserID.String() {
		return CreateDirectOutput{}, ErrSelfDirectChat
	}

	if _, err := useCase.userRepository.FindByID(ctx, relatedUserID); err != nil {
		if errors.Is(err, ports.ErrUserNotFound) {
			return CreateDirectOutput{}, ErrRelatedUserNotFound
		}

		return CreateDirectOutput{}, fmt.Errorf("find related user: %w",
			err)
	}
	rawChatID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return CreateDirectOutput{}, fmt.Errorf("generate chat id: %w",
			err)
	}

	chatID, err := chat.ParseID(rawChatID)
	if err != nil {
		return CreateDirectOutput{}, fmt.Errorf(
			"parse generated chat id: %w",
			err,
		)
	}

	now := useCase.clock.Now().UTC()

	candidate, err := chat.NewDirect(chatID, currentUserID, relatedUserID, now)
	if err != nil {
		return CreateDirectOutput{}, fmt.Errorf(
			"create direct chat entity: %w",
			err,
		)
	}
	storedChat, created, err := useCase.chatRepository.GetOrCreateDirect(ctx, candidate)
	if err != nil {
		return CreateDirectOutput{}, fmt.Errorf("get or create direct chat: %w",
			err)
	}
	storedRelatedUserID, err := storedChat.RelatedUserID(currentUserID)
	if err != nil {
		return CreateDirectOutput{}, fmt.Errorf(
			"resolve related user from stored chat: %w",
			err,
		)
	}

	return CreateDirectOutput{
		ID:            storedChat.ID().String(),
		RelatedUser:   storedRelatedUserID.String(),
		CreatedAt:     storedChat.CreatedAt(),
		UpdatedAt:     storedChat.UpdatedAt(),
		SummarySortAt: storedChat.SummarySortAt(),
		Created:       created,
	}, nil
}
