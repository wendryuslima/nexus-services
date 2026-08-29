package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/wendryuslima/nexus-services/internal/application/auth"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/request"
)

type SigninExecutor interface {
	Execute(
		ctx context.Context,
		input auth.SigninInput,
	) (auth.SigninOutput, error)
}

type SigninCookieManager interface {
	SetTokens(
		writer http.ResponseWriter,
		accessToken string,
		accessExpiresAt time.Time,
		refreshToken string,
		refreshExpiresAt time.Time,
	) error
}

type signinRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type signinResponse struct {
	Data signinResponseData `json:"data"`
}

type signinResponseData struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

type SigninHandler struct {
	useCase       SigninExecutor
	cookieManager SigninCookieManager
	logger        *slog.Logger
}

func NewSigninHandler(
	useCase SigninExecutor,
	cookieManager SigninCookieManager,
	logger *slog.Logger,
) (*SigninHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}

	if cookieManager == nil {
		return nil, ErrNilCookieManager
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &SigninHandler{
		useCase:       useCase,
		cookieManager: cookieManager,
		logger:        logger,
	}, nil
}

func (handler *SigninHandler) ServeHTTP(
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

	var body signinRequest

	if err := request.DecodeJSON(
		writer,
		httpRequest,
		&body,
		authJSONBodyLimit,
	); err != nil {
		handleJSONRequestError(
			handler.logger,
			writer,
			err,
		)

		return
	}

	output, err := handler.useCase.Execute(
		httpRequest.Context(),
		auth.SigninInput{
			Email:    body.Email,
			Password: body.Password,
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
			"failed to set signin authentication cookies",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/auth/signin"),
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

	writePayload(
		handler.logger,
		writer,
		http.StatusOK,
		signinResponse{
			Data: signinResponseData{
				UserID: output.UserID,
				Email:  output.Email,
			},
		},
	)
}

func (handler *SigninHandler) handleUseCaseError(
	writer http.ResponseWriter,
	httpRequest *http.Request,
	err error,
) {
	switch {
	case errors.Is(
		err,
		auth.ErrInvalidCredentials,
	):
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnauthorized,
			"invalid_credentials",
			"Email or password is incorrect.",
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
			"signin use case failed",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("route", "/v1/auth/signin"),
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
