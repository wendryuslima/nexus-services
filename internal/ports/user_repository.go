package ports

import (
	"context"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type UserListCursor struct {
	ID        user.ID
	CreatedAt time.Time
}

type ListUsersParams struct {
	Limit int
	After *UserListCursor
}

type ListUsersResult struct {
	Users      []*user.User
	HasNext    bool
	NextCursor *UserListCursor
	TotalItems int64
}

type UserRepository interface {
	Create(ctx context.Context, account *user.User) error
	FindByEmail(ctx context.Context, email user.Email) (*user.User, error)
	FindByID(ctx context.Context, id user.ID) (*user.User, error)
	FindByIDs(ctx context.Context, ids []user.ID) ([]*user.User, error)
	List(ctx context.Context, params ListUsersParams) (ListUsersResult, error)
}
