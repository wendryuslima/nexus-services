package chats

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	applicationchats "github.com/wendryuslima/nexus-services/internal/application/chats"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/request"
)

type CreateDirectExecutor interface {
	Execute(ctx context.Context, input applicationchats.CreateDirectInput) (applicationchats.CreateDirectOutput, error)
}

type createDirectRequest struct {
	RelatedUserID string `json:"relatedUserId"`
}

type createDirectResponse struct {
	Data createDirectResponseData `json:"data"`
}

type createDirectResponseData struct {
	ID            string    `json:"id"`
	RelatedUser   string    `json:"relatedUser"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	SummarySortAt time.Time `json:"summarySortAt"`
}

type CreateDirectHandler struct {
	useCase CreateDirectExecutor
	logger  *slog.Logger
}

func NewCreateDirectHandler(useCase CreateDirectExecutor, logger *slog.Logger) (*CreateDirectHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &CreateDirectHandler{
		useCase: useCase,
		logger:  logger,
	}, nil
}

func (handler *CreateDirectHandler) ServeHTTP(writer http.ResponseWriter, httpRequest *http.Request) {
	if httpRequest.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)

		writePublicError(handler.logger, writer, http.StatusMethodNotAllowed, "method_not_allowed", "Esta ação não está disponível")
		return
	}
	identity, found := authentication.IdentityFromContext(httpRequest.Context())
	if !found || strings.TrimSpace(identity.UserID) == "" {
		writePublicError(handler.logger, writer, http.StatusUnauthorized, "unauthenticated", "Faça login para continuar")
		return
	}

	var body createDirectRequest

	if err := request.DecodeJSON(writer, httpRequest, &body, chatJSONBodyLimit); err != nil {
		handleJSONRequestError(handler.logger, writer, err)
		return
	}

	output, err := handler.useCase.Execute(httpRequest.Context(), applicationchats.CreateDirectInput{
		CurrentUserID: identity.UserID,
		RelatedUserID: body.RelatedUserID,
	})
	if err != nil {
		handler.handleUseCaseError(writer, httpRequest, err)
		return
	}
	status := http.StatusOK
	if output.Created {
		status = http.StatusCreated
	}
	writePayload(handler.logger, writer, status, createDirectResponse{
		Data: createDirectResponseData{
			ID:            output.ID,
			RelatedUser:   output.RelatedUser,
			CreatedAt:     output.CreatedAt,
			UpdatedAt:     output.UpdatedAt,
			SummarySortAt: output.SummarySortAt,
		},
	})
}

func (handler *CreateDirectHandler) handleUseCaseError(writer http.ResponseWriter, httpRequest *http.Request, err error) {
	switch {
	case errors.Is(
		err,
		applicationchats.ErrInvalidRelatedUserID,
	):
		writePublicError(handler.logger,
			writer,
			http.StatusUnprocessableEntity,
			"invalid_related_user",
			"Informe um usuário válido.")
	case errors.Is(err, applicationchats.ErrSelfDirectChat):
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnprocessableEntity,
			"self_direct_chat_not_allowed",
			"Não é possível iniciar um chat direto consigo mesmo.",
		)
	case errors.Is(err, applicationchats.ErrRelatedUserNotFound):
		writePublicError(handler.logger, writer, http.StatusNotFound, "related_user_not_found", "O usuário informado não foi encontrado")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writePublicError(handler.logger, writer, http.StatusGatewayTimeout, "request_timeout", "Não foi possíbel concluir a solicitação a tempo")
	default:
		handler.logger.ErrorContext(httpRequest.Context(), "create direct chat use case failed", slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/chats/direct"))
		writePublicError(handler.logger,
			writer,
			http.StatusInternalServerError,
			"internal_error",
			"Não foi possível concluir a solicitação.")
	}
}
