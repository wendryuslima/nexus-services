package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type SignupInput struct {
	Email    string
	Password string
}

type SignupOutput struct {
	UserID    string
	Email     string
	CreatedAt time.Time
}

type SignupUseCase struct {
	userRepository ports.UserRepository
	passwordHasher ports.PasswordHasher
	idGenerator    ports.IDGenerator
	clock          ports.Clock
}

func NewSignupUseCase(userRepository ports.UserRepository, passwordHasher ports.PasswordHasher, idGenerator ports.IDGenerator, clock ports.Clock) (*SignupUseCase, error) {
	if userRepository == nil {
		return nil, fmt.Errorf(
			"%w: user repository",
			ErrNilDependency,
		)
	}

	if passwordHasher == nil {
		return nil, fmt.Errorf(
			"%w: password hasher",
			ErrNilDependency,
		)
	}

	if idGenerator == nil {
		return nil, fmt.Errorf(
			"%w: id generator",
			ErrNilDependency,
		)
	}

	if clock == nil {
		return nil, fmt.Errorf(
			"%w: clock",
			ErrNilDependency,
		)
	}

	return &SignupUseCase{
		userRepository: userRepository,
		passwordHasher: passwordHasher,
		idGenerator:    idGenerator,
		clock:          clock,
	}, nil

}

func (useCase *SignupUseCase) Execute(ctx context.Context, input SignupInput) (SignupOutput, error) {
	email, err := user.ParseEmail(input.Email)
	if err != nil {
		return SignupOutput{}, fmt.Errorf(
			"parse signup email: %w",
			err,
		)
	}

	if err := user.ValidatePlainPassword(input.Password); err != nil {
		return SignupOutput{}, fmt.Errorf("validate signup password: %w", err)
	}

	passwordHash, err := useCase.passwordHasher.Hash(ctx, input.Password)
	if err != nil {
		return SignupOutput{}, fmt.Errorf("hash signup password: %w", err)
	}

	rawUserID, err := useCase.idGenerator.New(ctx)
	if err != nil {
		return SignupOutput{}, fmt.Errorf("generate user id: %w", err)
	}
	userID, err := user.ParseID(rawUserID)
	if err != nil {
		return SignupOutput{}, fmt.Errorf("parse generated user id: %w", err)
	}
	createdAt := useCase.clock.Now().UTC()

	account, err := user.New(userID, email, passwordHash, createdAt)
	if err != nil {
		return SignupOutput{}, fmt.Errorf("create user entity: %w", err)
	}
	if err := useCase.userRepository.Create(ctx, account); err != nil {
		if errors.Is(err, ports.ErrEmailAlreadyExists) {
			return SignupOutput{}, ErrEmailAlreadyRegistred
		}
		return SignupOutput{}, fmt.Errorf("persist signup user: %w", err)
	}

	return SignupOutput{
		UserID:    account.ID().String(),
		Email:     account.Email().String(),
		CreatedAt: account.CreatedAt(),
	}, nil
}
