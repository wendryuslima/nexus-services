package chats

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/wendryuslima/nexus-services/internal/drivers/http/request"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

const chatJSONBodyLimit int64 = 4 * 1024

func handleJSONRequestError(logger *slog.Logger, writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, request.ErrUnsupportedMediaType):
		writePublicError(
			logger,
			writer,
			http.StatusUnsupportedMediaType,
			"unsupported_media_type",
			"O corpo da solicitação deve usar application/json.",
		)
	case errors.Is(err, request.ErrBodyTooLarge):
		writePublicError(
			logger,
			writer,
			http.StatusRequestEntityTooLarge,
			"request_body_too_large",
			"Os dados enviados excedem o tamanho permitido.",
		)

	default:
		writePublicError(
			logger,
			writer,
			http.StatusBadRequest,
			"invalid_request",
			"Não foi possível processar os dados enviados.",
		)
		writePublicError(
			logger,
			writer,
			http.StatusBadRequest,
			"invalid_request",
			"Não foi possível processar os dados enviados.",
		)
	}
}

func writePublicError(logger *slog.Logger, writer http.ResponseWriter, status int, code string, message string) {
	if err := response.WriteError(writer, status, code, message); err != nil {
		logger.Error(
			"failed to write chat HTTP error response",
			slog.Any("error", err),
			slog.Int("status", status),
			slog.String("code", code),
		)
	}
}

func writePayload(logger *slog.Logger, writer http.ResponseWriter, status int, payload any) {
	if err := response.WriteJSON(writer, status, payload); err != nil {
		logger.Error("failed to write chat HTTP JSON response",
			slog.Any("error", err),
			slog.Int("status", status))
	}
}
