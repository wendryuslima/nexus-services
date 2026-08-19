package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type SigninInput struct {
	Email    string
	Password string
}

type SigninOutput struct {
	UserID                string
	Email                 string
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

type SigninConfig struct {
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

type SigninDependencies struct {
	UserRepository    ports.UserRepository
	SessionRepository ports.SessionRepository
	PasswordHasher    ports.PasswordHasher
	TokenManager      ports.TokenManager
	TokenDigester     ports.TokenDigester
	IDGenerator       ports.IDGenerator
	Clock             ports.Clock
}

type SigninUseCase struct {
	userRepository    ports.UserRepository
	sessionRepository ports.SessionRepository
	passwordHasher    ports.PasswordHasher
	tokenManager      ports.TokenManager
	tokenDigester     ports.TokenDigester
	idGenerator       ports.IDGenerator
	clock             ports.Clock
	config            SigninConfig
}

func NewSigninUseCase(
	dependencies SigninDependencies,
	config SigninConfig,
) (*SigninUseCase, error) {
	if dependencies.UserRepository == nil {
		return nil, fmt.Errorf(
			"%w: user repository",
			ErrNilDependency,
		)
	}

	if dependencies.SessionRepository == nil {
		return nil, fmt.Errorf(
			"%w: session repository",
			ErrNilDependency,
		)
	}

	if dependencies.PasswordHasher == nil {
		return nil, fmt.Errorf(
			"%w: password hasher",
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

	return &SigninUseCase{
		userRepository:    dependencies.UserRepository,
		sessionRepository: dependencies.SessionRepository,
		passwordHasher:    dependencies.PasswordHasher,
		tokenManager:      dependencies.TokenManager,
		tokenDigester:     dependencies.TokenDigester,
		idGenerator:       dependencies.IDGenerator,
		clock:             dependencies.Clock,
		config:            config,
	}, nil
}

func (useCase *SigninUseCase) Execute(
	ctx context.Context,
	input SigninInput,
) (SigninOutput, error) {
	email, err := user.ParseEmail(input.Email)
	if err != nil {
		return SigninOutput{}, ErrInvalidCredentials
	}

	if err := user.ValidatePlainPassword(input.Password); err != nil {
		return SigninOutput{}, ErrInvalidCredentials
	}

	account, err := useCase.userRepository.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ports.ErrUserNotFound) {
			if err := useCase.consumePasswordHashCost(
				ctx,
				input.Password,
			); err != nil {
				return SigninOutput{}, fmt.Errorf(
					"consume password hashing cost: %w",
					err,
				)
			}

			return SigninOutput{}, ErrInvalidCredentials
		}

		return SigninOutput{}, fmt.Errorf(
			"find signin user: %w",
			err,
		)
	}

	passwordMatches, err := useCase.passwordHasher.Matches(
		ctx,
		input.Password,
		account.PasswordHash(),
	)
	if err != nil {
		return SigninOutput{}, fmt.Errorf(
			"compare signin password: %w",
			err,
		)
	}

	if !passwordMatches {
		return SigninOutput{}, ErrInvalidCredentials
	}

	sessionID, err := useCase.generateSessionID(ctx)
	if err != nil {
		return SigninOutput{}, err
	}

	accessTokenID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return SigninOutput{}, fmt.Errorf(
			"generate access token id: %w",
			err,
		)
	}

	refreshTokenID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return SigninOutput{}, fmt.Errorf(
			"generate refresh token id: %w",
			err,
		)
	}

	now := useCase.clock.Now().UTC()
	accessTokenExpiresAt := now.Add(useCase.config.AccessTokenTTL)
	refreshTokenExpiresAt := now.Add(useCase.config.RefreshTokenTTL)

	accessToken, err := useCase.tokenManager.SignAccessToken(
		ctx,
		ports.TokenClaims{
			TokenID:   accessTokenID,
			UserID:    account.ID(),
			SessionID: sessionID,
			IssuedAt:  now,
			ExpiresAt: accessTokenExpiresAt,
		},
	)
	if err != nil {
		return SigninOutput{}, fmt.Errorf(
			"sign access token: %w",
			err,
		)
	}

	refreshToken, err := useCase.tokenManager.SignRefreshToken(
		ctx,
		ports.TokenClaims{
			TokenID:   refreshTokenID,
			UserID:    account.ID(),
			SessionID: sessionID,
			IssuedAt:  now,
			ExpiresAt: refreshTokenExpiresAt,
		},
	)
	if err != nil {
		return SigninOutput{}, fmt.Errorf(
			"sign refresh token: %w",
			err,
		)
	}

	refreshTokenDigest, err := useCase.tokenDigester.Digest(
		ctx,
		refreshToken,
	)
	if err != nil {
		return SigninOutput{}, fmt.Errorf(
			"digest refresh token: %w",
			err,
		)
	}

	authSession, err := session.New(
		sessionID,
		account.ID(),
		refreshTokenDigest,
		now,
		refreshTokenExpiresAt,
	)
	if err != nil {
		return SigninOutput{}, fmt.Errorf(
			"create signin session: %w",
			err,
		)
	}

	if err := useCase.sessionRepository.Create(
		ctx,
		authSession,
	); err != nil {
		return SigninOutput{}, fmt.Errorf(
			"persist signin session: %w",
			err,
		)
	}

	return SigninOutput{
		UserID:                account.ID().String(),
		Email:                 account.Email().String(),
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessTokenExpiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}, nil
}

func (useCase *SigninUseCase) generateSessionID(
	ctx context.Context,
) (session.ID, error) {
	rawSessionID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return session.ID{}, fmt.Errorf(
			"generate session id: %w",
			err,
		)
	}

	sessionID, err := session.ParseID(rawSessionID)
	if err != nil {
		return session.ID{}, fmt.Errorf(
			"parse generated session id: %w",
			err,
		)
	}

	return sessionID, nil
}

func (useCase *SigninUseCase) consumePasswordHashCost(
	ctx context.Context,
	password string,
) error {
	_, err := useCase.passwordHasher.Hash(ctx, password)
	return err
}
