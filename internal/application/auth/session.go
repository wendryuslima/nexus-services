package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type SessionInput struct {
	UserID          string
	AccessExpiresAt time.Time
}

type SessionOutput struct {
	UserID          string
	Email           string
	AccessExpiresAt time.Time
}

type SessionUseCase struct {
	users ports.UserRepository
	clock ports.Clock
}

func NewSessionUseCase(users ports.UserRepository, clock ports.Clock) (*SessionUseCase, error) {
	if users == nil || clock == nil {
		return nil, ErrNilDependency
	}
	return &SessionUseCase{users: users, clock: clock}, nil
}

func (useCase *SessionUseCase) Execute(ctx context.Context, input SessionInput) (SessionOutput, error) {
	id, err := user.ParseID(input.UserID)
	if err != nil || !input.AccessExpiresAt.After(useCase.clock.Now().UTC()) {
		return SessionOutput{}, ErrUnauthenticated
	}
	account, err := useCase.users.FindByID(ctx, id)
	if errors.Is(err, ports.ErrUserNotFound) || err == nil && account == nil {
		return SessionOutput{}, ErrUnauthenticated
	}
	if err != nil {
		return SessionOutput{}, fmt.Errorf("read session account: %w", err)
	}
	return SessionOutput{UserID: account.ID().String(), Email: account.Email().String(), AccessExpiresAt: input.AccessExpiresAt.UTC()}, nil
}
