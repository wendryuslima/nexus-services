package users

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	applicationusers "github.com/wendryuslima/nexus-services/internal/application/users"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

type ListExecutor interface {
	Execute(ctx context.Context) (applicationusers.ListOutput, error)
}

type listResponse struct {
	Data []listUserResponse `json:"data"`
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

		handler.writePublicError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "Esta ação não está disponível")
		return
	}

	output, err := handler.useCase.Execute(httpRequest.Context())
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
	handler.writePayload(writer, http.StatusOK, listResponse{
		Data: users,
	})
}

func (handler *ListHandler) handleUseCaseError(writer http.ResponseWriter, httpRequest *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		handler.writePublicError(writer, http.StatusGatewayTimeout, "request_timeout", "Não foi possível concluir a solicitação a tempo. Tente novamente")
	default:
		handler.logger.ErrorContext(httpRequest.Context(), "list users use case failed", slog.Any("error", err), slog.String("method", httpRequest.Method), slog.String("route", "/v1/users"))
		handler.writePublicError(writer, http.StatusInternalServerError, "internal_error", "Não foi possível conclur a solicitação. Tente novamente mais tarde")
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
