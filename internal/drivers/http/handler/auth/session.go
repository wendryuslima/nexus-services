package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	applicationauth "github.com/wendryuslima/nexus-services/internal/application/auth"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
)

type SessionExecutor interface {
	Execute(context.Context, applicationauth.SessionInput) (applicationauth.SessionOutput, error)
}

type SessionHandler struct {
	useCase SessionExecutor
	logger  *slog.Logger
}

func NewSessionHandler(useCase SessionExecutor, logger *slog.Logger) (*SessionHandler, error) {
	if useCase == nil || logger == nil {
		return nil, errors.New("session handler dependencies cannot be nil")
	}
	return &SessionHandler{useCase: useCase, logger: logger}, nil
}

func (handler *SessionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writePublicError(handler.logger, w, 405, "method_not_allowed", "Esta ação não está disponível.")
		return
	}
	identity, ok := authentication.IdentityFromContext(r.Context())
	if !ok {
		writePublicError(handler.logger, w, 401, "unauthenticated", "Faça login para continuar.")
		return
	}
	output, err := handler.useCase.Execute(r.Context(), applicationauth.SessionInput{UserID: identity.UserID, AccessExpiresAt: identity.AccessExpiresAt})
	if errors.Is(err, applicationauth.ErrUnauthenticated) {
		writePublicError(handler.logger, w, 401, "unauthenticated", "Faça login para continuar.")
		return
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			writePublicError(handler.logger, w, 503, "session_unavailable", "Não foi possível consultar a sessão.")
		}
		return
	}
	payload := struct {
		Data struct {
			User struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"user"`
			AccessExpiresAt time.Time `json:"accessExpiresAt"`
		} `json:"data"`
	}{}
	payload.Data.User.ID = output.UserID
	payload.Data.User.Email = output.Email
	payload.Data.AccessExpiresAt = output.AccessExpiresAt
	writePayload(handler.logger, w, 200, payload)
}
