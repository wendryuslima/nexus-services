package chats

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	applicationchats "github.com/wendryuslima/nexus-services/internal/application/chats"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/request"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type MarkReadExecutor interface {
	Execute(context.Context, applicationchats.MarkReadInput) (ports.ChatReadState, error)
}
type MarkReadHandler struct {
	useCase MarkReadExecutor
	logger  *slog.Logger
}

func NewMarkReadHandler(useCase MarkReadExecutor, logger *slog.Logger) (*MarkReadHandler, error) {
	if useCase == nil {
		return nil, ErrNilUseCase
	}
	if logger == nil {
		return nil, ErrNilLogger
	}
	return &MarkReadHandler{useCase: useCase, logger: logger}, nil
}
func (h *MarkReadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writePublicError(h.logger, w, http.StatusMethodNotAllowed, "method_not_allowed", "Esta ação não está disponível.")
		return
	}
	identity, ok := authentication.IdentityFromContext(r.Context())
	if !ok || strings.TrimSpace(identity.UserID) == "" {
		writePublicError(h.logger, w, http.StatusUnauthorized, "unauthenticated", "Faça login para continuar.")
		return
	}
	var body struct {
		ThroughSequence *int64 `json:"throughSequence"`
	}
	if err := request.DecodeJSON(w, r, &body, chatJSONBodyLimit); err != nil {
		handleJSONRequestError(h.logger, w, err)
		return
	}
	if body.ThroughSequence == nil {
		writePublicError(h.logger, w, http.StatusBadRequest, "invalid_request", "Informe throughSequence.")
		return
	}
	state, err := h.useCase.Execute(r.Context(), applicationchats.MarkReadInput{ChatID: r.PathValue("chatId"), CurrentUserID: identity.UserID, ThroughSequence: *body.ThroughSequence})
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrChatNotFound):
			writePublicError(h.logger, w, http.StatusNotFound, "chat_not_found", "Conversa não encontrada.")
		case errors.Is(err, ports.ErrInvalidReadSequence):
			writePublicError(h.logger, w, http.StatusUnprocessableEntity, "invalid_read_sequence", "A sequência de leitura é inválida.")
		case errors.Is(err, context.DeadlineExceeded):
			writePublicError(h.logger, w, http.StatusGatewayTimeout, "request_timeout", "O tempo da solicitação foi excedido.")
		default:
			h.logger.Error("mark chat read failed", slog.Any("error", err))
			writePublicError(h.logger, w, http.StatusInternalServerError, "internal_error", "Não foi possível registrar a leitura.")
		}
		return
	}
	writePayload(h.logger, w, http.StatusOK, struct {
		Data ports.ChatReadState `json:"data"`
	}{state})
}
