package ports

import (
	"context"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type UserRepository interface {
	Create(ctx context.Context, account *user.User) error

	FindByEmail(ctx context.Context, email user.Email) (*user.User, error)

	FindByID(ctx context.Context, id user.ID) (*user.User, error)
}
