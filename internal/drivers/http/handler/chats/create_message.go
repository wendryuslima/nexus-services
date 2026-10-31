package chats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	applicationmessages "github.com/wendryuslima/nexus-services/internal/application/messages"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/request"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	createMessagePathPrefix = "/v1/chats/"
	createMessagePathSuffix = "/messages"

	createMessageJSONBodyLimit int64 = 20 * 1024
)

var _ http.Handler = (*CreateMessageHandler)(nil)

type CreateMessageExecutor interface {
	Execute(ctx context.Context, input applicationmessages.CreateInput) (applicationmessages.CreateOutput, error)
}

type createMessageContentRequest struct {
	Type    string `json:"type"`
	Value   string `json:"value,omitempty"`
	ImageID string `json:"imageId,omitempty"`
	Caption string `json:"caption,omitempty"`
}

type createMessageRequest struct {
	ClientMessageID string                      `json:"clientMessageId"`
	Content         createMessageContentRequest `json:"content"`
}

type createMessageResponse struct {
	Data createMessageResponseData `json:"data"`
}

type createMessageResponseData struct {
	ID              string                       `json:"id"`
	ClientMessageID string                       `json:"clientMessageId"`
	ChatID          string                       `json:"chatId"`
	Sequence        int64                        `json:"sequence"`
	SenderID        string                       `json:"senderId"`
	Content         createMessageContentResponse `json:"content"`
	CreatedAt       time.Time                    `json:"createdAt"`
	DeliveryStatus  string                       `json:"deliveryStatus"`
}

type createMessageContentResponse struct {
	Type    string `json:"type"`
	Value   string `json:"value,omitempty"`
	ImageID string `json:"imageId,omitempty"`
	Caption string `json:"caption,omitempty"`
}

type CreateMessageHandler struct {
	useCase     CreateMessageExecutor
	rateLimiter ports.MessageRateLimiter
	logger      *slog.Logger
}

func NewCreateMessageHandler(
	useCase CreateMessageExecutor,
	rateLimiter ports.MessageRateLimiter,
	logger *slog.Logger,
) (*CreateMessageHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}
	if logger == nil {
		return nil, ErrNilLogger
	}
	if rateLimiter == nil {
		return nil, ErrNilRateLimiter
	}

	return &CreateMessageHandler{
		useCase:     useCase,
		rateLimiter: rateLimiter,
		logger:      logger,
	}, nil
}

func (handler *CreateMessageHandler) ServeHTTP(
	writer http.ResponseWriter,
	httpRequest *http.Request,
) {
	chatID, ok := createMessageChatIDFromPath(httpRequest.URL.Path)
	if !ok {
		writePublicError(
			handler.logger,
			writer,
			http.StatusNotFound,
			"route_not_found",
			"A página solicitada não foi encontrada.",
		)
		return
	}

	if httpRequest.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writePublicError(
			handler.logger,
			writer,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Esta ação não está disponível.",
		)
		return
	}

	identity, found := authentication.IdentityFromContext(httpRequest.Context())
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

	if !handler.reserveSendAttempt(writer, httpRequest, identity.UserID) {
		return
	}

	var body createMessageRequest
	if err := request.DecodeJSON(
		writer,
		httpRequest,
		&body,
		createMessageJSONBodyLimit,
	); err != nil {
		handleJSONRequestError(handler.logger, writer, err)
		return
	}

	output, err := handler.useCase.Execute(
		httpRequest.Context(),
		applicationmessages.CreateInput{
			CurrentUserID:   identity.UserID,
			ChatID:          chatID,
			ClientMessageID: body.ClientMessageID,
			Content: applicationmessages.CreateContentInput{
				Type:  body.Content.Type,
				Value: body.Content.Value, ImageID: body.Content.ImageID, Caption: body.Content.Caption,
			},
		},
	)
	if err != nil {
		handler.handleUseCaseError(writer, httpRequest, err)
		return
	}

	status := http.StatusOK
	if output.Created {
		status = http.StatusCreated
	}

	writePayload(handler.logger, writer, status, mapCreateMessageResponse(output))
}

func (handler *CreateMessageHandler) reserveSendAttempt(writer http.ResponseWriter, httpRequest *http.Request, userID string) bool {
	retryAfter, err := handler.rateLimiter.Reserve(httpRequest.Context(), userID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		if errors.Is(err, context.DeadlineExceeded) {
			handler.handleUseCaseError(writer, httpRequest, err)
			return false
		}
		handler.logger.ErrorContext(httpRequest.Context(), "reserve message rate limit failed", slog.Any("error", err))
		writer.Header().Set("Retry-After", "1")
		writePublicError(handler.logger, writer, http.StatusServiceUnavailable,
			"message_rate_limit_unavailable", "Não foi possível enviar a mensagem agora. Tente novamente em instantes.")
		return false
	}
	if retryAfter > 0 {
		seconds := int64((retryAfter + time.Second - 1) / time.Second)
		writer.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		writePublicError(handler.logger, writer, http.StatusTooManyRequests,
			"message_rate_limit_exceeded", fmt.Sprintf("Você enviou mensagens muito rápido. Aguarde %d segundos.", seconds))
		return false
	}
	return true
}

func createMessageChatIDFromPath(path string) (string, bool) {
	if !strings.HasPrefix(path, createMessagePathPrefix) ||
		!strings.HasSuffix(path, createMessagePathSuffix) {
		return "", false
	}

	chatID := strings.TrimSuffix(
		strings.TrimPrefix(path, createMessagePathPrefix),
		createMessagePathSuffix,
	)
	if strings.TrimSpace(chatID) == "" || strings.Contains(chatID, "/") {
		return "", false
	}

	return chatID, true
}

func mapCreateMessageResponse(output applicationmessages.CreateOutput) createMessageResponse {
	return createMessageResponse{
		Data: createMessageResponseData{
			ID:              output.ID,
			ClientMessageID: output.ClientMessageID,
			ChatID:          output.ChatID,
			Sequence:        output.Sequence,
			SenderID:        output.SenderID,
			Content: createMessageContentResponse{
				Type:  output.Content.Type,
				Value: output.Content.Value, ImageID: output.Content.ImageID, Caption: output.Content.Caption,
			},
			CreatedAt:      output.CreatedAt,
			DeliveryStatus: string(output.DeliveryStatus),
		},
	}
}

func (handler *CreateMessageHandler) handleUseCaseError(
	writer http.ResponseWriter,
	httpRequest *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, ports.ErrImageUnavailable):
		writePublicError(handler.logger, writer, http.StatusConflict, "image_unavailable", "A imagem não está disponível para esta conversa. Selecione a imagem novamente.")
	case errors.Is(err, applicationmessages.ErrInvalidCurrentUserID):
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnauthorized,
			"unauthenticated",
			"Faça login para continuar.",
		)
	case errors.Is(err, applicationmessages.ErrInvalidChatID),
		errors.Is(err, applicationmessages.ErrChatNotFound):
		writePublicError(
			handler.logger,
			writer,
			http.StatusNotFound,
			"chat_not_found",
			"O chat informado não foi encontrado.",
		)
	case errors.Is(err, applicationmessages.ErrInvalidClientMessageID):
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnprocessableEntity,
			"invalid_client_message_id",
			"Informe um identificador de mensagem válido.",
		)
	case errors.Is(err, applicationmessages.ErrInvalidContentType),
		errors.Is(err, applicationmessages.ErrInvalidContent):
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnprocessableEntity,
			"invalid_message_content",
			"Informe um conteúdo de mensagem válido.",
		)
	case errors.Is(err, applicationmessages.ErrContentTooLong):
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnprocessableEntity,
			"message_content_too_long",
			"O conteúdo da mensagem excede o tamanho permitido.",
		)
	case errors.Is(err, applicationmessages.ErrIdempotencyConflict):
		writePublicError(
			handler.logger,
			writer,
			http.StatusConflict,
			"idempotency_conflict",
			"O identificador da mensagem já foi usado com outro conteúdo.",
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
			"create message use case failed",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/chats/{chatId}/messages"),
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
