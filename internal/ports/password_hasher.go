package ports

import (
	"context"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type PasswordHasher interface {
	Hash(ctx context.Context, plainPassword string) (user.PasswordHash, error)
	Matches(ctx context.Context, plainPassword string, passwordHash user.PasswordHash) (bool, error)
}
