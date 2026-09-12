package users

import (
	"context"
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
)

type ListedUser struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

type ListOutput struct {
	Users []ListedUser
}

type ListUseCase struct {
	userRepository ports.UserRepository
}

func NewListUseCase(userRepository ports.UserRepository) (*ListUseCase, error) {
	if userRepository == nil {
		return nil, fmt.Errorf("%w: user repository", ErrNilDependency)
	}
	return &ListUseCase{
		userRepository: userRepository,
	}, nil
}

func (useCase *ListUseCase) Execute(ctx context.Context) (ListOutput, error) {
	accounts, err := useCase.userRepository.FindAll(ctx)
	if err != nil {
		return ListOutput{}, fmt.Errorf("list user: %w", err)
	}
	listedUsers := make([]ListedUser, 0, len(accounts))
	for _, account := range accounts {
		listedUsers = append(listedUsers, ListedUser{
			ID:        account.ID().String(),
			Email:     account.Email().String(),
			CreatedAt: account.CreatedAt(),
		})
	}
	return ListOutput{
		Users: listedUsers,
	}, nil
}
