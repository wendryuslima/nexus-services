package authentication

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	applicationauth "github.com/wendryuslima/nexus-services/internal/application/auth"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

type AuthenticateExecutor interface {
	Execute(ctx context.Context, input applicationauth.AuthenticateInput) (applicationauth.AuthenticateOutput, error)
}

type AccessTokenReader interface {
	AccessToken(httpRequest *http.Request) (string, error)
}

type Middleware struct {
	authenticator     AuthenticateExecutor
	accessTokenReader AccessTokenReader
	logger            *slog.Logger
}

func New(authenticator AuthenticateExecutor, accessTokenReader AccessTokenReader, logger *slog.Logger) (*Middleware, error) {
	if authenticator == nil {
		return nil, ErrNilAuthenticator
	}

	if accessTokenReader == nil {
		return nil, ErrNilAccessTokenReader
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &Middleware{
		authenticator:     authenticator,
		accessTokenReader: accessTokenReader,
		logger:            logger,
	}, nil
}

func (middleware *Middleware) Wrap(next http.Handler) (http.Handler, error) {
	if next == nil {
		return nil, ErrNilHandler
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		accessToken, err := middleware.accessTokenReader.AccessToken(httpRequest)
		if err != nil {
			middleware.writePublicError(writer, http.StatusUnauthorized, "unauthenticated", "Faça login para continuar.")
			return
		}
		output, err := middleware.authenticator.Execute(httpRequest.Context(), applicationauth.AuthenticateInput{
			AccessToken: accessToken,
		})
		if err != nil {
			middleware.handleAuthenticationError(writer, httpRequest, err)
			return
		}

		identity := Identity{
			UserID:    output.UserID,
			SessionID: output.SessionID,
		}
		authenticatedContext := withIdentity(httpRequest.Context(), identity)
		next.ServeHTTP(writer, httpRequest.WithContext(authenticatedContext))
	}), nil
}

func (middleware *Middleware) handleAuthenticationError(writer http.ResponseWriter, httpRequest *http.Request, err error) {
	switch {
	case errors.Is(err, applicationauth.ErrUnauthenticated):
		middleware.writePublicError(writer, http.StatusUnauthorized, "unauthenticated", "Faça login para continuar.")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		middleware.writePublicError(writer, http.StatusGatewayTimeout, "request_timeout", "Não foi possível validar sua autenticação a tempo.")
	default:
		middleware.logger.ErrorContext(
			httpRequest.Context(),
			"request authentication failed",
			slog.Any("error", err),
			slog.String("method", httpRequest.Method),
			slog.String("path", httpRequest.URL.Path),
		)
		middleware.writePublicError(writer, http.StatusInternalServerError, "internal_error", "Não foi possível concluir a solicitação")

	}

}

func (middleware *Middleware) writePublicError(writer http.ResponseWriter, status int, code string, message string) {
	if err := response.WriteError(writer, status, code, message); err != nil {
		middleware.logger.Error(
			"failed to write authentication error response",
			slog.Any("error", err),
			slog.Int("status", status),
			slog.String("code", code),
		)
	}
}
