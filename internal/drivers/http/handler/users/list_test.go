package users

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	applicationusers "github.com/wendryuslima/nexus-services/internal/application/users"
)

type listExecutorStub struct {
	input applicationusers.ListInput
	calls int
}

func (stub *listExecutorStub) Execute(_ context.Context, input applicationusers.ListInput) (applicationusers.ListOutput, error) {
	stub.input = input
	stub.calls++
	return applicationusers.ListOutput{Users: []applicationusers.ListedUser{{
		ID: "user-1", Email: "user@example.test", CreatedAt: time.Date(2026, 10, 4, 3, 22, 12, 0, time.UTC),
	}}, Pagination: applicationusers.ListPaginationOutput{
		HasNext: true, NextCursor: &applicationusers.ListCursorOutput{
			ID: "user-1", CreatedAt: time.Date(2026, 10, 4, 3, 22, 12, 0, time.UTC),
		}, Page: 1, PageSize: 30, TotalItems: 31, TotalPages: 2,
	}}, nil
}

func TestListHandlerReturnsPagination(t *testing.T) {
	stub := &listExecutorStub{}
	handler, _ := NewListHandler(stub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/users?page=1&pageSize=30", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
	var payload struct {
		Data []struct {
			UserID string `json:"user_id"`
		} `json:"data"`
		Pagination struct {
			HasNext    bool `json:"hasNext"`
			NextCursor *struct {
				ID string `json:"id"`
			} `json:"nextCursor"`
			TotalPages int64 `json:"totalPages"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 || stub.input.Page != 1 || payload.Data[0].UserID != "user-1" ||
		!payload.Pagination.HasNext || payload.Pagination.NextCursor.ID != "user-1" || payload.Pagination.TotalPages != 2 {
		t.Fatalf("unexpected response: %+v", payload)
	}
}

func TestListHandlerRejectsRepeatedPage(t *testing.T) {
	stub := &listExecutorStub{}
	handler, _ := NewListHandler(stub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/users?page=1&page=2", nil))
	if recorder.Code != http.StatusBadRequest || stub.calls != 0 {
		t.Fatalf("unexpected status %d or use case calls %d", recorder.Code, stub.calls)
	}
}
