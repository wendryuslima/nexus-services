package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/wendryuslima/nexus-services/internal/application/auth"
)

type RefreshExecutor interface {
	Execute(
		ctx context.Context,
		input auth.RefreshInput,
	) (auth.RefreshOutput, error)
}

type RefreshCookieManager interface {
	RefreshToken(
		httpRequest *http.Request,
	) (string, error)

	SetTokens(
		writer http.ResponseWriter,
		accessToken string,
		accessExpiresAt time.Time,
		refreshToken string,
		refreshExpiresAt time.Time,
	) error

	Clear(writer http.ResponseWriter) error
}

type RefreshHandler struct {
	useCase       RefreshExecutor
	cookieManager RefreshCookieManager
	logger        *slog.Logger
}

func NewRefreshHandler(
	useCase RefreshExecutor,
	cookieManager RefreshCookieManager,
	logger *slog.Logger,
) (*RefreshHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}

	if cookieManager == nil {
		return nil, ErrNilCookieManager
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &RefreshHandler{
		useCase:       useCase,
		cookieManager: cookieManager,
		logger:        logger,
	}, nil
}

func (handler *RefreshHandler) ServeHTTP(
	writer http.ResponseWriter,
	httpRequest *http.Request,
) {
	if httpRequest.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)

		writePublicError(
			handler.logger,
			writer,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"The requested method is not allowed.",
		)

		return
	}

	refreshToken, err := handler.cookieManager.RefreshToken(
		httpRequest,
	)
	if err != nil {
		handler.respondInvalidRefresh(
			writer,
			httpRequest,
		)

		return
	}

	output, err := handler.useCase.Execute(
		httpRequest.Context(),
		auth.RefreshInput{
			RefreshToken: refreshToken,
		},
	)
	if err != nil {
		handler.handleUseCaseError(
			writer,
			httpRequest,
			err,
		)

		return
	}

	if err := handler.cookieManager.SetTokens(
		writer,
		output.AccessToken,
		output.AccessTokenExpiresAt,
		output.RefreshToken,
		output.RefreshTokenExpiresAt,
	); err != nil {
		handler.logger.ErrorContext(
			httpRequest.Context(),
			"failed to set refreshed authentication cookies",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/auth/refresh"),
		)

		handler.clearCookies(
			writer,
			httpRequest,
		)

		writePublicError(
			handler.logger,
			writer,
			http.StatusInternalServerError,
			"internal_error",
			"An internal error occurred.",
		)

		return
	}

	writeNoContent(writer)
}

func (handler *RefreshHandler) handleUseCaseError(
	writer http.ResponseWriter,
	httpRequest *http.Request,
	err error,
) {
	switch {
	case errors.Is(
		err,
		auth.ErrInvalidRefreshToken,
	):
		handler.respondInvalidRefresh(
			writer,
			httpRequest,
		)

	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		writePublicError(
			handler.logger,
			writer,
			http.StatusGatewayTimeout,
			"request_timeout",
			"The request could not be completed in time.",
		)

	default:
		handler.logger.ErrorContext(
			httpRequest.Context(),
			"refresh use case failed",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/auth/refresh"),
		)

		writePublicError(
			handler.logger,
			writer,
			http.StatusInternalServerError,
			"internal_error",
			"An internal error occurred.",
		)
	}
}

func (handler *RefreshHandler) respondInvalidRefresh(
	writer http.ResponseWriter,
	httpRequest *http.Request,
) {
	handler.clearCookies(
		writer,
		httpRequest,
	)

	writePublicError(
		handler.logger,
		writer,
		http.StatusUnauthorized,
		"invalid_refresh_token",
		"The authentication session is no longer valid.",
	)
}

func (handler *RefreshHandler) clearCookies(
	writer http.ResponseWriter,
	httpRequest *http.Request,
) {
	if err := handler.cookieManager.Clear(writer); err != nil {
		handler.logger.ErrorContext(
			httpRequest.Context(),
			"failed to clear authentication cookies",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/auth/refresh"),
		)
	}
}
