package chats

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

	applicationchats "github.com/wendryuslima/nexus-services/internal/application/chats"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
)

type ListExecutor interface {
	Execute(
		ctx context.Context,
		input applicationchats.ListInput,
	) (applicationchats.ListOutput, error)
}

type listResponse struct {
	Data       []listChatResponse     `json:"data"`
	Pagination listPaginationResponse `json:"pagination"`
}

type listChatResponse struct {
	ID               string    `json:"id"`
	RelatedUser      string    `json:"relatedUser"`
	RelatedUserEmail string    `json:"relatedUserEmail"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	SummarySortAt    time.Time `json:"summarySortAt"`
}

type listPaginationResponse struct {
	HasNext    bool                    `json:"hasNext"`
	NextCursor *listNextCursorResponse `json:"nextCursor"`
	Page       int                     `json:"page"`
	PageSize   int                     `json:"pageSize"`
	TotalItems int64                   `json:"totalItems"`
	TotalPages int64                   `json:"totalPages"`
}

type listNextCursorResponse struct {
	ID     string    `json:"id"`
	SortAt time.Time `json:"sortAt"`
}

type ListHandler struct {
	useCase ListExecutor
	logger  *slog.Logger
}

func NewListHandler(
	useCase ListExecutor,
	logger *slog.Logger,
) (*ListHandler, error) {
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

func (handler *ListHandler) ServeHTTP(
	writer http.ResponseWriter,
	httpRequest *http.Request,
) {
	if httpRequest.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)

		writePublicError(
			handler.logger,
			writer,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Esta ação não está disponível.",
		)
		return
	}

	identity, found := authentication.IdentityFromContext(
		httpRequest.Context(),
	)
	if !found || strings.TrimSpace(identity.UserID) == "" {
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"Faça login para continuar.",
		)
		return
	}

	input, err := parseListInput(
		httpRequest.URL.Query(),
		identity.UserID,
	)
	if err != nil {
		writePublicError(
			handler.logger,
			writer,
			http.StatusBadRequest,
			"invalid_pagination",
			"Os parâmetros de paginação são inválidos.",
		)
		return
	}

	output, err := handler.useCase.Execute(
		httpRequest.Context(),
		input,
	)
	if err != nil {
		handler.handleUseCaseError(
			writer,
			httpRequest,
			err,
		)
		return
	}

	writePayload(
		handler.logger,
		writer,
		http.StatusOK,
		mapListResponse(output),
	)
}

func parseListInput(
	query url.Values,
	currentUserID string,
) (applicationchats.ListInput, error) {
	page, err := parseOptionalIntegerQuery(
		query,
		"page",
	)
	if err != nil {
		return applicationchats.ListInput{}, err
	}

	pageSize, err := parseOptionalIntegerQuery(
		query,
		"pageSize",
	)
	if err != nil {
		return applicationchats.ListInput{}, err
	}

	cursorID, hasCursorID, err := optionalSingleQueryValue(
		query,
		"cursorId",
	)
	if err != nil {
		return applicationchats.ListInput{}, err
	}

	cursorSortAt, hasCursorSortAt, err := optionalSingleQueryValue(
		query,
		"cursorSortAt",
	)
	if err != nil {
		return applicationchats.ListInput{}, err
	}

	var cursor *applicationchats.ListCursorInput

	if hasCursorID || hasCursorSortAt {
		cursor = &applicationchats.ListCursorInput{
			ID:     cursorID,
			SortAt: cursorSortAt,
		}
	}

	return applicationchats.ListInput{
		CurrentUserID: currentUserID,
		Page:          page,
		PageSize:      pageSize,
		Cursor:        cursor,
	}, nil
}

func parseOptionalIntegerQuery(
	query url.Values,
	key string,
) (int, error) {
	rawValue, found, err := optionalSingleQueryValue(
		query,
		key,
	)
	if err != nil {
		return 0, err
	}

	if !found {
		return 0, nil
	}

	if rawValue == "" {
		return 0, fmt.Errorf(
			"%w: %s is empty",
			ErrInvalidQueryParameter,
			key,
		)
	}

	value, err := strconv.Atoi(rawValue)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: %s must be an integer",
			ErrInvalidQueryParameter,
			key,
		)
	}

	return value, nil
}

func optionalSingleQueryValue(
	query url.Values,
	key string,
) (string, bool, error) {
	values, found := query[key]
	if !found {
		return "", false, nil
	}

	if len(values) != 1 {
		return "", true, fmt.Errorf(
			"%w: %s must appear once",
			ErrInvalidQueryParameter,
			key,
		)
	}

	return strings.TrimSpace(values[0]), true, nil
}

func mapListResponse(
	output applicationchats.ListOutput,
) listResponse {
	items := make(
		[]listChatResponse,
		0,
		len(output.Chats),
	)

	for _, listedChat := range output.Chats {
		items = append(
			items,
			listChatResponse{
				ID:               listedChat.ID,
				RelatedUser:      listedChat.RelatedUser,
				RelatedUserEmail: listedChat.RelatedUserEmail,
				CreatedAt:        listedChat.CreatedAt,
				UpdatedAt:        listedChat.UpdatedAt,
				SummarySortAt:    listedChat.SummarySortAt,
			},
		)
	}

	var nextCursor *listNextCursorResponse

	if output.Pagination.NextCursor != nil {
		nextCursor = &listNextCursorResponse{
			ID:     output.Pagination.NextCursor.ID,
			SortAt: output.Pagination.NextCursor.SortAt,
		}
	}

	return listResponse{
		Data: items,
		Pagination: listPaginationResponse{
			HasNext:    output.Pagination.HasNext,
			NextCursor: nextCursor,
			Page:       output.Pagination.Page,
			PageSize:   output.Pagination.PageSize,
			TotalItems: output.Pagination.TotalItems,
			TotalPages: output.Pagination.TotalPages,
		},
	}
}

func (handler *ListHandler) handleUseCaseError(
	writer http.ResponseWriter,
	httpRequest *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, applicationchats.ErrInvalidPage),
		errors.Is(err, applicationchats.ErrInvalidPageSize),
		errors.Is(err, applicationchats.ErrInvalidCursor),
		errors.Is(err, applicationchats.ErrInconsistentPagination):
		writePublicError(
			handler.logger,
			writer,
			http.StatusBadRequest,
			"invalid_pagination",
			"Os parâmetros de paginação são inválidos.",
		)

	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		writePublicError(
			handler.logger,
			writer,
			http.StatusGatewayTimeout,
			"request_timeout",
			"Não foi possível concluir a solicitação a tempo.",
		)

	default:
		handler.logger.ErrorContext(
			httpRequest.Context(),
			"list chats use case failed",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/chats"),
		)

		writePublicError(
			handler.logger,
			writer,
			http.StatusInternalServerError,
			"internal_error",
			"Não foi possível concluir a solicitação.",
		)
	}
}
