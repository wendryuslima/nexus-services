package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type AuthenticateInput struct {
	AccessToken string
}

type AuthenticateOutput struct {
	UserID           string
	SessionID        string
	AccessExpiresAt  time.Time
	SessionExpiresAt time.Time
}

type AuthenticateDependencies struct {
	SessionRepository ports.SessionRepository
	TokenManager      ports.TokenManager
	Clock             ports.Clock
}

type AuthenticateUseCase struct {
	sessionRepository ports.SessionRepository
	tokenManager      ports.TokenManager
	clock             ports.Clock
}

func NewAuthenticateUseCase(
	dependencies AuthenticateDependencies,
) (*AuthenticateUseCase, error) {
	if dependencies.SessionRepository == nil {
		return nil, fmt.Errorf(
			"%w: session repository",
			ErrNilDependency,
		)
	}

	if dependencies.TokenManager == nil {
		return nil, fmt.Errorf(
			"%w: token manager",
			ErrNilDependency,
		)
	}

	if dependencies.Clock == nil {
		return nil, fmt.Errorf(
			"%w: clock",
			ErrNilDependency,
		)
	}

	return &AuthenticateUseCase{
		sessionRepository: dependencies.SessionRepository,
		tokenManager:      dependencies.TokenManager,
		clock:             dependencies.Clock,
	}, nil
}

func (useCase *AuthenticateUseCase) Execute(
	ctx context.Context,
	input AuthenticateInput,
) (AuthenticateOutput, error) {
	if strings.TrimSpace(input.AccessToken) == "" {
		return AuthenticateOutput{}, ErrUnauthenticated
	}

	claims, err := useCase.tokenManager.VerifyAccessToken(
		ctx,
		input.AccessToken,
	)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidToken) ||
			errors.Is(err, ports.ErrExpiredToken) {
			return AuthenticateOutput{}, ErrUnauthenticated
		}

		return AuthenticateOutput{}, fmt.Errorf(
			"verify access token: %w",
			err,
		)
	}

	if claims.UserID.String() == "" ||
		claims.SessionID.String() == "" {
		return AuthenticateOutput{}, ErrUnauthenticated
	}

	authSession, err := useCase.sessionRepository.FindByID(
		ctx,
		claims.SessionID,
	)
	if err != nil {
		if errors.Is(err, ports.ErrSessionNotFound) {
			return AuthenticateOutput{}, ErrUnauthenticated
		}

		return AuthenticateOutput{}, fmt.Errorf(
			"find authentication session: %w",
			err,
		)
	}

	if authSession == nil || authSession.UserID().String() != claims.UserID.String() || !claims.ExpiresAt.After(useCase.clock.Now().UTC()) {
		return AuthenticateOutput{}, ErrUnauthenticated
	}

	if err := authSession.EnsureActive(
		useCase.clock.Now().UTC(),
	); err != nil {
		if errors.Is(err, session.ErrExpired) ||
			errors.Is(err, session.ErrRevoked) {
			return AuthenticateOutput{}, ErrUnauthenticated
		}

		return AuthenticateOutput{}, fmt.Errorf(
			"validate authentication session: %w",
			err,
		)
	}

	return AuthenticateOutput{
		UserID:           claims.UserID.String(),
		SessionID:        claims.SessionID.String(),
		AccessExpiresAt:  claims.ExpiresAt.UTC(),
		SessionExpiresAt: authSession.ExpiresAt().UTC(),
	}, nil
}
