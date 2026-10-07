package users

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	applicationusers "github.com/wendryuslima/nexus-services/internal/application/users"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

type ListExecutor interface {
	Execute(ctx context.Context, input applicationusers.ListInput) (applicationusers.ListOutput, error)
}

type listResponse struct {
	Data       []listUserResponse     `json:"data"`
	Pagination listPaginationResponse `json:"pagination"`
}

type listPaginationResponse struct {
	HasNext    bool                `json:"hasNext"`
	NextCursor *listCursorResponse `json:"nextCursor"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"pageSize"`
	TotalItems int64               `json:"totalItems"`
	TotalPages int64               `json:"totalPages"`
}

type listCursorResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

type listUserResponse struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type ListHandler struct {
	useCase ListExecutor
	logger  *slog.Logger
}

func NewListHandler(useCase ListExecutor, logger *slog.Logger) (*ListHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}
	if logger == nil {
		return nil, ErrNilLogger
	}

	return &ListHandler{
		useCase: useCase,
		logger:  logger,
	}, nil
}

func (handler *ListHandler) ServeHTTP(writer http.ResponseWriter, httpRequest *http.Request) {
	if httpRequest.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)

		handler.writePublicError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "Esta ação não está disponível.")
		return
	}

	input, err := parseListInput(httpRequest.URL.Query())
	if err != nil {
		handler.writePublicError(writer, http.StatusBadRequest, "invalid_pagination", "Os parâmetros de paginação são inválidos.")
		return
	}
	output, err := handler.useCase.Execute(httpRequest.Context(), input)
	if err != nil {
		handler.handleUseCaseError(writer, httpRequest, err)
		return
	}
	users := make([]listUserResponse, 0, len(output.Users))
	for _, listedUser := range output.Users {
		users = append(users, listUserResponse{
			UserID:    listedUser.ID,
			Email:     listedUser.Email,
			CreatedAt: listedUser.CreatedAt,
		})
	}
	var nextCursor *listCursorResponse
	if output.Pagination.NextCursor != nil {
		nextCursor = &listCursorResponse{ID: output.Pagination.NextCursor.ID, CreatedAt: output.Pagination.NextCursor.CreatedAt}
	}
	handler.writePayload(writer, http.StatusOK, listResponse{Data: users, Pagination: listPaginationResponse{
		HasNext: output.Pagination.HasNext, NextCursor: nextCursor,
		Page: output.Pagination.Page, PageSize: output.Pagination.PageSize,
		TotalItems: output.Pagination.TotalItems, TotalPages: output.Pagination.TotalPages,
	}})
}

func parseListInput(query url.Values) (applicationusers.ListInput, error) {
	page, err := parseInteger(query, "page")
	if err != nil {
		return applicationusers.ListInput{}, err
	}
	pageSize, err := parseInteger(query, "pageSize")
	if err != nil {
		return applicationusers.ListInput{}, err
	}
	cursorID, hasID, err := singleValue(query, "cursorId")
	if err != nil {
		return applicationusers.ListInput{}, err
	}
	cursorCreatedAt, hasCreatedAt, err := singleValue(query, "cursorCreatedAt")
	if err != nil {
		return applicationusers.ListInput{}, err
	}
	var cursor *applicationusers.ListCursorInput
	if hasID || hasCreatedAt {
		cursor = &applicationusers.ListCursorInput{ID: cursorID, CreatedAt: cursorCreatedAt}
	}
	return applicationusers.ListInput{Page: page, PageSize: pageSize, Cursor: cursor}, nil
}

func parseInteger(query url.Values, key string) (int, error) {
	value, found, err := singleValue(query, key)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	if value == "" {
		return 0, fmt.Errorf("%s is empty", key)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	if parsed < 1 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return parsed, nil
}

func singleValue(query url.Values, key string) (string, bool, error) {
	values, found := query[key]
	if !found {
		return "", false, nil
	}
	if len(values) != 1 {
		return "", true, fmt.Errorf("%s must appear once", key)
	}
	return strings.TrimSpace(values[0]), true, nil
}

func (handler *ListHandler) handleUseCaseError(writer http.ResponseWriter, httpRequest *http.Request, err error) {
	switch {
	case errors.Is(err, applicationusers.ErrInvalidPage), errors.Is(err, applicationusers.ErrInvalidPageSize),
		errors.Is(err, applicationusers.ErrInvalidCursor), errors.Is(err, applicationusers.ErrInconsistentPagination):
		handler.writePublicError(writer, http.StatusBadRequest, "invalid_pagination", "Os parâmetros de paginação são inválidos.")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		handler.writePublicError(writer, http.StatusGatewayTimeout, "request_timeout", "Não foi possível concluir a solicitação a tempo. Tente novamente.")
	default:
		handler.logger.ErrorContext(httpRequest.Context(), "list users use case failed", slog.Any("error", err), slog.String("method", httpRequest.Method), slog.String("route", "/v1/users"))
		handler.writePublicError(writer, http.StatusInternalServerError, "internal_error", "Não foi possível concluir a solicitação. Tente novamente mais tarde.")
	}

}

func (handler *ListHandler) writePublicError(writer http.ResponseWriter, status int, code string, message string) {
	if err := response.WriteError(writer, status, code, message); err != nil {
		handler.logger.Error("failed to write HTTP error response", slog.Any("error", err), slog.Int("status", status), slog.String("code", code))

	}

}

func (handler *ListHandler) writePayload(writer http.ResponseWriter, status int, payload any) {
	if err := response.WriteJSON(writer, status, payload); err != nil {
		handler.logger.Error("failed to write HTTP JSON response",
			slog.Any("error", err),
			slog.Int("status", status))
	}
}
