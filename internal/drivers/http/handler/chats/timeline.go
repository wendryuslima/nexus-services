package chats

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	applicationtimeline "github.com/wendryuslima/nexus-services/internal/application/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
)

const (
	timelinePathPrefix = "/v1/chats/"
	timelinePathSuffix = "/timeline"
)

type TimelineExecutor interface {
	Execute(context.Context, applicationtimeline.ListInput) (applicationtimeline.ListOutput, error)
}

type timelineResponse struct {
	Data timelineResponseData `json:"data"`
}

type timelineResponseData struct {
	SnapshotThroughSequence *int64                     `json:"snapshotThroughSequence,omitempty"`
	Items                   []timelineItemResponse     `json:"items"`
	Pagination              timelinePaginationResponse `json:"pagination"`
}

type timelineItemResponse struct {
	Sequence  int64                   `json:"sequence"`
	ID        string                  `json:"id"`
	Kind      string                  `json:"kind"`
	CreatedAt time.Time               `json:"createdAt"`
	UpdatedAt time.Time               `json:"updatedAt"`
	Message   timelineMessageResponse `json:"message"`
}

type timelineMessageResponse struct {
	ClientMessageID string                  `json:"clientMessageId,omitempty"`
	SenderID        string                  `json:"senderId"`
	IsMine          bool                    `json:"isMine"`
	Content         timelineContentResponse `json:"content"`
}

type timelineContentResponse struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type timelinePaginationResponse struct {
	NextBeforeSequence *int64                      `json:"nextBeforeSequence"`
	NextAfterSequence  *int64                      `json:"nextAfterSequence"`
	HasNext            bool                        `json:"hasNext"`
	NextCursor         *timelineNextCursorResponse `json:"nextCursor"`
	PageSize           int                         `json:"pageSize"`
}

type timelineNextCursorResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

type TimelineHandler struct {
	useCase TimelineExecutor
	logger  *slog.Logger
}

func NewTimelineHandler(useCase TimelineExecutor, logger *slog.Logger) (*TimelineHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}
	if logger == nil {
		return nil, ErrNilLogger
	}
	return &TimelineHandler{useCase: useCase, logger: logger}, nil
}

func (handler *TimelineHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	chatID, ok := timelineChatIDFromPath(request.URL.Path)
	if !ok {
		writePublicError(handler.logger, writer, http.StatusNotFound, "route_not_found", "A página solicitada não foi encontrada.")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writePublicError(handler.logger, writer, http.StatusMethodNotAllowed, "method_not_allowed", "Esta ação não está disponível.")
		return
	}

	identity, found := authentication.IdentityFromContext(request.Context())
	if !found || strings.TrimSpace(identity.UserID) == "" {
		writePublicError(handler.logger, writer, http.StatusUnauthorized, "unauthenticated", "Faça login para continuar.")
		return
	}
	input, err := parseTimelineInput(request.URL.Query(), identity.UserID, chatID)
	if err != nil {
		writePublicError(handler.logger, writer, http.StatusBadRequest, "invalid_pagination", "Os parâmetros de paginação são inválidos.")
		return
	}
	output, err := handler.useCase.Execute(request.Context(), input)
	if err != nil {
		handler.handleTimelineError(writer, request, err)
		return
	}
	writePayload(handler.logger, writer, http.StatusOK, mapTimelineResponse(output))
}

func timelineChatIDFromPath(path string) (string, bool) {
	if !strings.HasPrefix(path, timelinePathPrefix) || !strings.HasSuffix(path, timelinePathSuffix) {
		return "", false
	}
	chatID := strings.TrimSuffix(strings.TrimPrefix(path, timelinePathPrefix), timelinePathSuffix)
	if strings.TrimSpace(chatID) == "" || strings.Contains(chatID, "/") {
		return "", false
	}
	return chatID, true
}

func parseTimelineInput(query url.Values, currentUserID string, chatID string) (applicationtimeline.ListInput, error) {
	for _, values := range query {
		if len(values) != 1 {
			return applicationtimeline.ListInput{}, ErrInvalidQueryParameter
		}
	}
	if query.Has("pageSize") {
		n, err := strconv.Atoi(query.Get("pageSize"))
		if err != nil || n < 1 || n > 100 {
			return applicationtimeline.ListInput{}, ErrInvalidQueryParameter
		}
	}
	pageSize, err := parseOptionalIntegerQuery(query, "pageSize")
	if err != nil {
		return applicationtimeline.ListInput{}, err
	}
	cursorID, hasCursorID, err := optionalSingleQueryValue(query, "cursorId")
	if err != nil {
		return applicationtimeline.ListInput{}, err
	}
	cursorCreatedAt, hasCursorCreatedAt, err := optionalSingleQueryValue(query, "cursorCreatedAt")
	if err != nil {
		return applicationtimeline.ListInput{}, err
	}
	if hasCursorID != hasCursorCreatedAt {
		return applicationtimeline.ListInput{}, ErrInvalidQueryParameter
	}

	var cursor *applicationtimeline.ListCursorInput
	if hasCursorID {
		cursor = &applicationtimeline.ListCursorInput{ID: cursorID, CreatedAt: cursorCreatedAt}
	}
	input := applicationtimeline.ListInput{
		CurrentUserID: currentUserID, ChatID: chatID, PageSize: pageSize, Cursor: cursor,
	}
	if query.Has("order") {
		if query.Get("order") != "sequence" {
			return input, ErrInvalidQueryParameter
		}
		input.SequenceOrder = true
	}
	for key, target := range map[string]**int64{"beforeSequence": &input.BeforeSequence, "afterSequence": &input.AfterSequence, "throughSequence": &input.ThroughSequence} {
		if query.Has(key) {
			raw := query.Get(key)
			if raw == "" || strings.IndexFunc(raw, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
				return input, ErrInvalidQueryParameter
			}
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value > timeline.MaxSequence {
				return input, ErrInvalidQueryParameter
			}
			*target = &value
		}
	}
	if cursor != nil && (input.SequenceOrder || input.BeforeSequence != nil || input.AfterSequence != nil || input.ThroughSequence != nil) ||
		input.BeforeSequence != nil && (!input.SequenceOrder || *input.BeforeSequence == 0) ||
		input.AfterSequence != nil && (input.SequenceOrder || input.BeforeSequence != nil) ||
		input.ThroughSequence != nil && (input.AfterSequence == nil || *input.AfterSequence > *input.ThroughSequence) {
		return input, ErrInvalidQueryParameter
	}
	return input, nil
}

func mapTimelineResponse(output applicationtimeline.ListOutput) timelineResponse {
	items := make([]timelineItemResponse, 0, len(output.Items))
	for _, item := range output.Items {
		items = append(items, timelineItemResponse{
			Sequence: item.Sequence,
			ID:       item.ID, Kind: item.Kind, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
			Message: timelineMessageResponse{
				ClientMessageID: item.Message.ClientMessageID,
				SenderID:        item.Message.SenderID, IsMine: item.Message.IsMine,
				Content: timelineContentResponse{Type: item.Message.Content.Type, Value: item.Message.Content.Value},
			},
		})
	}
	var nextCursor *timelineNextCursorResponse
	if output.Pagination.NextCursor != nil {
		nextCursor = &timelineNextCursorResponse{
			ID: output.Pagination.NextCursor.ID, CreatedAt: output.Pagination.NextCursor.CreatedAt,
		}
	}
	return timelineResponse{Data: timelineResponseData{
		SnapshotThroughSequence: output.SnapshotThroughSequence,
		Items:                   items,
		Pagination: timelinePaginationResponse{
			NextBeforeSequence: output.Pagination.NextBeforeSequence, NextAfterSequence: output.Pagination.NextAfterSequence,
			HasNext: output.Pagination.HasNext, NextCursor: nextCursor, PageSize: output.Pagination.PageSize,
		},
	}}
}

func (handler *TimelineHandler) handleTimelineError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, applicationtimeline.ErrInvalidPageSize), errors.Is(err, applicationtimeline.ErrInvalidCursor):
		writePublicError(handler.logger, writer, http.StatusBadRequest, "invalid_pagination", "Os parâmetros de paginação são inválidos.")
	case errors.Is(err, applicationtimeline.ErrInvalidChatID), errors.Is(err, applicationtimeline.ErrChatNotFound):
		writePublicError(handler.logger, writer, http.StatusNotFound, "chat_not_found", "O chat informado não foi encontrado.")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writePublicError(handler.logger, writer, http.StatusGatewayTimeout, "request_timeout", "Não foi possível concluir a solicitação a tempo.")
	default:
		handler.logger.ErrorContext(request.Context(), "list timeline use case failed",
			slog.Any("error", err), slog.String("method", request.Method), slog.String("route", "/v1/chats/{chatId}/timeline"))
		writePublicError(handler.logger, writer, http.StatusInternalServerError, "internal_error", "Não foi possível concluir a solicitação.")
	}
}
