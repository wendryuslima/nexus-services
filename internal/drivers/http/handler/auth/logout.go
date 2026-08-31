package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wendryuslima/nexus-services/internal/application/auth"
)

type LogoutExecutor interface {
	Execute(ctx context.Context, input auth.LogoutInput) error
}

type LogoutCookieManager interface {
	RefreshToken(httpRequest *http.Request) (string, error)
	Clear(writer http.ResponseWriter) error
}

type LogoutHandler struct {
	useCase       LogoutExecutor
	cookieManager LogoutCookieManager
	logger        *slog.Logger
}

func NewLogoutHandler(useCase LogoutExecutor, cookieManager LogoutCookieManager, logger *slog.Logger) (*LogoutHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}

	if cookieManager == nil {
		return nil, ErrNilCookieManager
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &LogoutHandler{
		useCase:       useCase,
		cookieManager: cookieManager,
		logger:        logger,
	}, nil
}

func (handler *LogoutHandler) ServeHTTP(writer http.ResponseWriter, httpRequest *http.Request) {
	if httpRequest.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)

		writePublicError(handler.logger, writer, http.StatusMethodNotAllowed, "method_not_allowed", "The requested method is not allowed")
		return
	}

	refreshToken, err := handler.cookieManager.RefreshToken(httpRequest)
	if err != nil {
		handler.completeLogout(writer, httpRequest)
		return
	}

	err = handler.useCase.Execute(httpRequest.Context(), auth.LogoutInput{
		RefreshToken: refreshToken,
	})
	if err != nil {
		handler.handleUseCaseError(writer, httpRequest, err)
		return
	}

	handler.completeLogout(writer, httpRequest)
}

func (handler *LogoutHandler) completeLogout(writer http.ResponseWriter, httpRequest *http.Request) {
	if err := handler.cookieManager.Clear(writer); err != nil {
		handler.logger.ErrorContext(httpRequest.Context(), "failed to clear logout authentication cookies", slog.Any("error", err), slog.String("method", httpRequest.Method), slog.String("route", "v1/auth/logout"))
		writePublicError(handler.logger, writer, http.StatusInternalServerError, "interal_error", "An internal error ocurred")
		return
	}
	writeNoContent(writer)
}

func (handler *LogoutHandler) handleUseCaseError(writer http.ResponseWriter, httpRequest *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writePublicError(handler.logger, writer, http.StatusGatewayTimeout, "request_timeout", "The request could not be completed in time.")
	default:
		handler.logger.ErrorContext(
			httpRequest.Context(),
			"logout use case failed",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/auth/logout"),
		)
		writePublicError(handler.logger, writer, http.StatusServiceUnavailable, "logout_unavailable", "Logout could not be completed. Please try again.")
	}
}
