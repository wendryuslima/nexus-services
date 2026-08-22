package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type RefreshInput struct {
	RefreshToken string
}

type RefreshOutput struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

type RefreshConfig struct {
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type RefreshDependencies struct {
	SessionRepository ports.SessionRepository
	TokenManager      ports.TokenManager
	TokenDigester     ports.TokenDigester
	IDGenerator       ports.IDGenerator
	Clock             ports.Clock
}

type RefreshUseCase struct {
	sessionRepository ports.SessionRepository
	tokenManager      ports.TokenManager
	tokenDigester     ports.TokenDigester
	idGenerator       ports.IDGenerator
	clock             ports.Clock
	config            RefreshConfig
}

func NewRefreshUseCase(
	dependencies RefreshDependencies,
	config RefreshConfig,
) (*RefreshUseCase, error) {
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

	if dependencies.TokenDigester == nil {
		return nil, fmt.Errorf(
			"%w: token digester",
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

	if config.AccessTokenTTL <= 0 {
		return nil, fmt.Errorf(
			"%w: access token TTL must be positive",
			ErrInvalidConfiguration,
		)
	}

	if config.RefreshTokenTTL <= config.AccessTokenTTL {
		return nil, fmt.Errorf(
			"%w: refresh token TTL must be greater than access token TTL",
			ErrInvalidConfiguration,
		)
	}

	return &RefreshUseCase{
		sessionRepository: dependencies.SessionRepository,
		tokenManager:      dependencies.TokenManager,
		tokenDigester:     dependencies.TokenDigester,
		idGenerator:       dependencies.IDGenerator,
		clock:             dependencies.Clock,
		config:            config,
	}, nil
}

func (useCase *RefreshUseCase) Execute(
	ctx context.Context,
	input RefreshInput,
) (RefreshOutput, error) {
	if input.RefreshToken == "" {
		return RefreshOutput{}, ErrInvalidRefreshToken
	}

	claims, err := useCase.tokenManager.VerifyRefreshToken(
		ctx,
		input.RefreshToken,
	)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidToken) ||
			errors.Is(err, ports.ErrExpiredToken) {
			return RefreshOutput{}, ErrInvalidRefreshToken
		}

		return RefreshOutput{}, fmt.Errorf(
			"verify refresh token: %w",
			err,
		)
	}

	if claims.UserID.String() == "" ||
		claims.SessionID.String() == "" {
		return RefreshOutput{}, ErrInvalidRefreshToken
	}

	authSession, err := useCase.sessionRepository.FindByID(
		ctx,
		claims.SessionID,
	)
	if err != nil {
		if errors.Is(err, ports.ErrSessionNotFound) {
			return RefreshOutput{}, ErrInvalidRefreshToken
		}

		return RefreshOutput{}, fmt.Errorf(
			"find refresh session: %w",
			err,
		)
	}

	if authSession.UserID().String() != claims.UserID.String() {
		return RefreshOutput{}, ErrInvalidRefreshToken
	}

	now := useCase.clock.Now().UTC()

	if err := authSession.EnsureActive(now); err != nil {
		return RefreshOutput{}, ErrInvalidRefreshToken
	}

	presentedDigest, err := useCase.tokenDigester.Digest(
		ctx,
		input.RefreshToken,
	)
	if err != nil {
		return RefreshOutput{}, fmt.Errorf(
			"digest presented refresh token: %w",
			err,
		)
	}

	expectedDigest := authSession.RefreshTokenDigest()

	if err := authSession.ValidateRefreshTokenDigest(
		presentedDigest,
	); err != nil {
		if errors.Is(err, session.ErrRefreshTokenMismatch) {
			return RefreshOutput{}, useCase.rejectReusedToken(
				ctx,
				authSession.ID(),
				now,
			)
		}

		return RefreshOutput{}, fmt.Errorf(
			"validate refresh token digest: %w",
			err,
		)
	}

	accessTokenID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return RefreshOutput{}, fmt.Errorf(
			"generate refreshed access token id: %w",
			err,
		)
	}

	refreshTokenID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return RefreshOutput{}, fmt.Errorf(
			"generate refreshed refresh token id: %w",
			err,
		)
	}

	accessTokenExpiresAt := now.Add(useCase.config.AccessTokenTTL)
	refreshTokenExpiresAt := now.Add(useCase.config.RefreshTokenTTL)

	accessToken, err := useCase.tokenManager.SignAccessToken(
		ctx,
		ports.TokenClaims{
			TokenID:   accessTokenID,
			UserID:    authSession.UserID(),
			SessionID: authSession.ID(),
			IssuedAt:  now,
			ExpiresAt: accessTokenExpiresAt,
		},
	)
	if err != nil {
		return RefreshOutput{}, fmt.Errorf(
			"sign refreshed access token: %w",
			err,
		)
	}

	refreshToken, err := useCase.tokenManager.SignRefreshToken(
		ctx,
		ports.TokenClaims{
			TokenID:   refreshTokenID,
			UserID:    authSession.UserID(),
			SessionID: authSession.ID(),
			IssuedAt:  now,
			ExpiresAt: refreshTokenExpiresAt,
		},
	)
	if err != nil {
		return RefreshOutput{}, fmt.Errorf(
			"sign refreshed refresh token: %w",
			err,
		)
	}

	newRefreshTokenDigest, err := useCase.tokenDigester.Digest(
		ctx,
		refreshToken,
	)
	if err != nil {
		return RefreshOutput{}, fmt.Errorf(
			"digest new refresh token: %w",
			err,
		)
	}

	if err := authSession.RotateRefreshToken(
		newRefreshTokenDigest,
		refreshTokenExpiresAt,
		now,
	); err != nil {
		return RefreshOutput{}, fmt.Errorf(
			"rotate refresh session entity: %w",
			err,
		)
	}

	if err := useCase.sessionRepository.Rotate(
		ctx,
		authSession,
		expectedDigest,
		now,
	); err != nil {

		if errors.Is(err, ports.ErrSessionConflict) {
			return RefreshOutput{}, useCase.rejectReusedToken(
				ctx,
				authSession.ID(),
				now,
			)
		}

		return RefreshOutput{}, fmt.Errorf(
			"persist refresh token rotation: %w",
			err,
		)
	}

	return RefreshOutput{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessTokenExpiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}, nil
}

func (useCase *RefreshUseCase) rejectReusedToken(
	ctx context.Context,
	sessionID session.ID,
	revokedAt time.Time,
) error {
	if err := useCase.sessionRepository.Revoke(
		ctx,
		sessionID,
		revokedAt,
	); err != nil {
		return fmt.Errorf(
			"revoke session after refresh token reuse: %w",
			err,
		)
	}

	return ErrInvalidRefreshToken
}
