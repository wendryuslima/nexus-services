package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/wendryuslima/nexus-services/internal/drivers/http/request"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

const authJSONBodyLimite int64 = 16 * 1024

func writeNoContent(writer http.ResponseWriter) {
	writer.Header().Set("Cache-control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func handleJSONRequestError(logger *slog.Logger, writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, request.ErrUnsupportedMediaType):
		writePublicError(logger, writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
	case errors.Is(err, request.ErrBodyTooLarge):
		writePublicError(logger, writer, http.StatusRequestEntityTooLarge, "request_body_too_large", "The request body is too large.")
	default:
		writePublicError(
			logger,
			writer,
			http.StatusBadRequest,
			"invalid_request",
			"The request body is invalid",
		)
	}
}

func writePublicError(logger *slog.Logger, writer http.ResponseWriter, status int, code string, message string) {
	if err := response.WriteError(writer, status, code, message); err != nil {
		logger.Error("failed to write HTTP error response", slog.Any("error", err), slog.Int("status", status), slog.String("code", code))
	}
}

func writePayload(logger *slog.Logger, writer http.ResponseWriter, status int, payload any) {
	if err := response.WriteJSON(writer, status, payload); err != nil {
		logger.Error("failed to write HTTP JSON response", slog.Any("error", err), slog.Int("status", status))

	}

}
