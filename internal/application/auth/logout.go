package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/ports"
)

type LogoutInput struct {
	RefreshToken string
}

type LogoutDependencies struct {
	SessionRepository ports.SessionRepository
	TokenManager      ports.TokenManager
	Clock             ports.Clock
}

type LogoutUseCase struct {
	sessionRepository ports.SessionRepository
	tokenManager      ports.TokenManager
	clock             ports.Clock
}

func NewLogoutUseCase(dependencies LogoutDependencies) (*LogoutUseCase, error) {
	if dependencies.SessionRepository == nil {
		return nil, fmt.Errorf("%w: session repository", ErrNilDependency)
	}
	if dependencies.TokenManager == nil {
		return nil, fmt.Errorf("%w: token manager", ErrNilDependency)
	}
	if dependencies.Clock == nil {
		return nil, fmt.Errorf("%w: clock", ErrNilDependency)
	}

	return &LogoutUseCase{
		sessionRepository: dependencies.SessionRepository,
		tokenManager:      dependencies.TokenManager,
		clock:             dependencies.Clock,
	}, nil
}

func (useCase *LogoutUseCase) Execute(ctx context.Context, input LogoutInput) error {
	if input.RefreshToken == "" {
		return nil
	}

	claims, err := useCase.tokenManager.VerifyAccessToken(ctx, input.RefreshToken)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidToken) ||
			errors.Is(err, ports.ErrExpiredToken) {
			return nil
		}

		return fmt.Errorf("verify logout refresh token: %w", err)
	}
	if claims.UserID.String() == "" || claims.SessionID.String() == "" {
		return nil
	}

	authSession, err := useCase.sessionRepository.FindByID(ctx, claims.SessionID)
	if err != nil {
		if errors.Is(err, ports.ErrSessionNotFound) {
			return nil
		}

		return fmt.Errorf("find logout session: %w", err)
	}

	if authSession.UserID().String() != claims.UserID.String() {
		return nil
	}

	if err := useCase.sessionRepository.Revoke(ctx, authSession.ID(), useCase.clock.Now().UTC()); err != nil {
		if errors.Is(err, ports.ErrSessionNotFound) {
			return nil
		}

		return fmt.Errorf("revoke logout session: %w", err)
	}

	return nil

}
