package users

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type listRepositoryStub struct {
	params ports.ListUsersParams
	result ports.ListUsersResult
	calls  int
}

func (stub *listRepositoryStub) Create(context.Context, *user.User) error { return nil }
func (stub *listRepositoryStub) FindByEmail(context.Context, user.Email) (*user.User, error) {
	return nil, nil
}
func (stub *listRepositoryStub) FindByID(context.Context, user.ID) (*user.User, error) {
	return nil, nil
}
func (stub *listRepositoryStub) FindByIDs(context.Context, []user.ID) ([]*user.User, error) {
	return nil, nil
}
func (stub *listRepositoryStub) List(_ context.Context, params ports.ListUsersParams) (ports.ListUsersResult, error) {
	stub.calls++
	stub.params = params
	return stub.result, nil
}

func TestListUsesCursorAndKeepsTimestamp(t *testing.T) {
	createdAt := time.Date(2026, 10, 4, 3, 22, 12, 916000000, time.UTC)
	id, _ := user.ParseID("user-1")
	email, _ := user.ParseEmail("user@example.test")
	hash, _ := user.NewPasswordHash("hash")
	account, _ := user.New(id, email, hash, createdAt)
	repository := &listRepositoryStub{result: ports.ListUsersResult{
		Users: []*user.User{account}, HasNext: true,
		NextCursor: &ports.UserListCursor{ID: id, CreatedAt: createdAt}, TotalItems: 31,
	}}
	useCase, _ := NewListUseCase(repository)
	output, err := useCase.Execute(context.Background(), ListInput{Page: 2, PageSize: 30,
		Cursor: &ListCursorInput{ID: "previous", CreatedAt: createdAt.Format(time.RFC3339Nano)}})
	if err != nil {
		t.Fatal(err)
	}
	if repository.params.After == nil || !repository.params.After.CreatedAt.Equal(createdAt) || repository.params.Limit != 30 {
		t.Fatalf("unexpected repository pagination: %+v", repository.params)
	}
	if output.Pagination.NextCursor == nil || output.Pagination.NextCursor.ID != "user-1" || output.Pagination.TotalPages != 2 {
		t.Fatalf("unexpected response pagination: %+v", output.Pagination)
	}
}

func TestListRejectsIncompleteCursorBeforeQuery(t *testing.T) {
	repository := &listRepositoryStub{}
	useCase, _ := NewListUseCase(repository)
	_, err := useCase.Execute(context.Background(), ListInput{Page: 2, Cursor: &ListCursorInput{ID: "user-1"}})
	if !errors.Is(err, ErrInvalidCursor) || repository.calls != 0 {
		t.Fatalf("expected invalid cursor without a query, got %v and %d calls", err, repository.calls)
	}
}
