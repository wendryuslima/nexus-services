package users

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	defaultPageSize = 30
	maxPageSize     = 100
)

type ListCursorInput struct {
	ID        string
	CreatedAt string
}

type ListInput struct {
	Page     int
	PageSize int
	Cursor   *ListCursorInput
}

type ListedUser struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

type ListCursorOutput struct {
	ID        string
	CreatedAt time.Time
}

type ListPaginationOutput struct {
	HasNext    bool
	NextCursor *ListCursorOutput
	Page       int
	PageSize   int
	TotalItems int64
	TotalPages int64
}

type ListOutput struct {
	Users      []ListedUser
	Pagination ListPaginationOutput
}

type ListUseCase struct {
	userRepository ports.UserRepository
}

func NewListUseCase(userRepository ports.UserRepository) (*ListUseCase, error) {
	if userRepository == nil {
		return nil, fmt.Errorf("%w: user repository", ErrNilDependency)
	}
	return &ListUseCase{userRepository: userRepository}, nil
}

func (useCase *ListUseCase) Execute(ctx context.Context, input ListInput) (ListOutput, error) {
	page := input.Page
	if page == 0 {
		page = 1
	}
	if page < 1 {
		return ListOutput{}, ErrInvalidPage
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if pageSize < 1 || pageSize > maxPageSize {
		return ListOutput{}, ErrInvalidPageSize
	}
	var cursor *ports.UserListCursor
	if input.Cursor != nil {
		id, err := user.ParseID(input.Cursor.ID)
		if err != nil {
			return ListOutput{}, ErrInvalidCursor
		}
		createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.Cursor.CreatedAt))
		if err != nil {
			return ListOutput{}, ErrInvalidCursor
		}
		cursor = &ports.UserListCursor{ID: id, CreatedAt: createdAt.UTC()}
	}
	if (page == 1 && cursor != nil) || (page > 1 && cursor == nil) {
		return ListOutput{}, ErrInconsistentPagination
	}
	result, err := useCase.userRepository.List(ctx, ports.ListUsersParams{Limit: pageSize, After: cursor})
	if err != nil {
		return ListOutput{}, fmt.Errorf("list users: %w", err)
	}
	listedUsers := make([]ListedUser, 0, len(result.Users))
	for _, account := range result.Users {
		if account == nil {
			return ListOutput{}, fmt.Errorf("user repository returned a nil user")
		}
		listedUsers = append(listedUsers, ListedUser{ID: account.ID().String(), Email: account.Email().String(), CreatedAt: account.CreatedAt()})
	}
	var nextCursor *ListCursorOutput
	if result.HasNext {
		if result.NextCursor == nil {
			return ListOutput{}, fmt.Errorf("user repository returned hasNext without a next cursor")
		}
		nextCursor = &ListCursorOutput{ID: result.NextCursor.ID.String(), CreatedAt: result.NextCursor.CreatedAt.UTC()}
	}
	totalPages := int64(0)
	if result.TotalItems > 0 {
		totalPages = (result.TotalItems + int64(pageSize) - 1) / int64(pageSize)
	}
	return ListOutput{Users: listedUsers, Pagination: ListPaginationOutput{
		HasNext: result.HasNext, NextCursor: nextCursor, Page: page, PageSize: pageSize,
		TotalItems: result.TotalItems, TotalPages: totalPages,
	}}, nil
}
