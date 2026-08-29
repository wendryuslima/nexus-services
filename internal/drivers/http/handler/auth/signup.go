package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/wendryuslima/nexus-services/internal/application/auth"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/request"
)

const authJSONBodyLimit int64 = 16 * 1024

type SignupExecutor interface {
	Execute(ctx context.Context, input auth.SignupInput) (auth.SignupOutput, error)
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type signupResponse struct {
	Data signupResponseData `json:"data"`
}

type signupResponseData struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type SignupHandler struct {
	useCase SignupExecutor
	logger  *slog.Logger
}

func NewSignupHandler(useCase SignupExecutor, logger *slog.Logger) (*SignupHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &SignupHandler{
		useCase: useCase,
		logger:  logger,
	}, nil

}

func (handler *SignupHandler) ServeHTTP(writer http.ResponseWriter, httpRequest *http.Request) {
	if httpRequest.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)

		writePublicError(handler.logger, writer, http.StatusMethodNotAllowed, "method_not_allowed", "The requested method is not allowed")
		return
	}

	var body signupRequest

	if err := request.DecodeJSON(writer, httpRequest, &body, authJSONBodyLimit); err != nil {
		handleJSONRequestError(
			handler.logger,
			writer,
			err,
		)
		return
	}

	output, err := handler.useCase.Execute(httpRequest.Context(), auth.SignupInput{
		Email:    body.Email,
		Password: body.Password,
	})

	if err != nil {
		handler.handleUseCaseError(writer, httpRequest, err)
		return
	}

	writePayload(handler.logger, writer, http.StatusCreated, signupResponse{
		Data: signupResponseData{
			UserID:    output.UserID,
			Email:     output.Email,
			CreatedAt: output.CreatedAt,
		},
	})

}

func (handler *SignupHandler) handleUseCaseError(writer http.ResponseWriter, httpRequest *http.Request, err error) {
	switch {
	case errors.Is(err, user.ErrInvalidEmail):
		writePublicError(handler.logger, writer, http.StatusUnprocessableEntity, "invalid_email", "The provided email is invalid.")
	case errors.Is(err, user.ErrInvalidPassword):
		writePublicError(
			handler.logger,
			writer,
			http.StatusUnprocessableEntity,
			"invalid_password",
			"The password does not meet the requirements",
		)
	case errors.Is(err, auth.ErrEmailAlreadyRegistred):
		writePublicError(handler.logger, writer, http.StatusConflict, "email_already_registered", "The email is already registered")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writePublicError(handler.logger, writer, http.StatusGatewayTimeout, "request_timeout", "The request could not be completed in time.")

	default:
		handler.logger.ErrorContext(httpRequest.Context(), "signup use case failed", slog.Any("error", err), slog.String("method", httpRequest.Method), slog.String("router", "/v1/auth/signup"))

		writePublicError(handler.logger, writer, http.StatusInternalServerError, "internal_error", "An internal")
	}
}
