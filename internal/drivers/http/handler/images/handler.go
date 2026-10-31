package images

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	applicationimages "github.com/wendryuslima/nexus-services/internal/application/images"
	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type Service interface {
	Upload(context.Context, applicationimages.UploadInput) (applicationimages.Metadata, error)
	Access(context.Context, string, string) (applicationimages.Access, error)
}

type Handler struct {
	service     Service
	logger      *slog.Logger
	uploads     chan struct{}
	rateLimiter ports.MessageRateLimiter
}

func New(service Service, rateLimiter ports.MessageRateLimiter, logger *slog.Logger) (*Handler, error) {
	if service == nil || rateLimiter == nil || logger == nil {
		return nil, errors.New("missing image handler dependency")
	}
	return &Handler{service: service, rateLimiter: rateLimiter, logger: logger, uploads: make(chan struct{}, 2)}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	identity, ok := authentication.IdentityFromContext(r.Context())
	if !ok || identity.UserID == "" {
		h.writeError(w, http.StatusUnauthorized, "unauthenticated", "Faça login para continuar.")
		return
	}
	if r.URL.Path == "/v1/images" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			h.writeError(w, 405, "method_not_allowed", "Esta ação não está disponível.")
			return
		}
		h.upload(w, r, identity.UserID)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/images/"), "/access")
	if !strings.HasPrefix(r.URL.Path, "/v1/images/") || !strings.HasSuffix(r.URL.Path, "/access") || id == "" || strings.Contains(id, "/") || len(id) > 128 {
		h.writeError(w, 404, "route_not_found", "A página solicitada não foi encontrada.")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		h.writeError(w, 405, "method_not_allowed", "Esta ação não está disponível.")
		return
	}
	access, err := h.service.Access(r.Context(), identity.UserID, id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.writeData(w, 200, access)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, userID string) {
	retryAfter, err := h.rateLimiter.Reserve(r.Context(), "image:"+userID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64((retryAfter+time.Second-1)/time.Second), 10))
		h.writeError(w, 429, "image_upload_rate_limit", "Você enviou imagens muito rápido. Aguarde antes de tentar novamente.")
		return
	}
	select {
	case h.uploads <- struct{}{}:
		defer func() { <-h.uploads }()
	default:
		w.Header().Set("Retry-After", "2")
		h.writeError(w, 429, "image_upload_limit", "Há muitos uploads em andamento. Tente novamente em instantes.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, domainimage.MaxBytes+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		h.writeError(w, 415, "unsupported_media_type", "Envie a imagem como multipart/form-data.")
		return
	}
	file, err := os.CreateTemp("", "nexus-image-*")
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	input := applicationimages.UploadInput{UserID: userID, Body: file}
	seen := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			h.multipartError(w, r, err)
			return
		}
		name := part.FormName()
		if seen[name] || (name != "chatId" && name != "clientUploadId" && name != "file") {
			h.writeError(w, 400, "invalid_image_upload", "Informe chatId, clientUploadId e uma única imagem.")
			return
		}
		seen[name] = true
		if name != "file" {
			value, err := io.ReadAll(io.LimitReader(part, 257))
			if err != nil {
				h.multipartError(w, r, err)
				return
			}
			if len(value) > 256 || part.FileName() != "" {
				h.writeError(w, 400, "invalid_image_upload", "Os dados do upload são inválidos.")
				return
			}
			if name == "chatId" {
				input.ChatID = string(value)
			} else {
				input.ClientUploadID = string(value)
			}
			continue
		}
		if part.FileName() == "" {
			h.writeError(w, 400, "invalid_image_upload", "Selecione uma imagem.")
			return
		}
		hash := sha256.New()
		size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(part, domainimage.MaxBytes+1))
		if err != nil {
			h.multipartError(w, r, err)
			return
		}
		if size > domainimage.MaxBytes {
			h.writeError(w, 413, "image_too_large", "A imagem deve ter no máximo 10 MiB.")
			return
		}
		input.SizeBytes = size
		input.Checksum = hex.EncodeToString(hash.Sum(nil))
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			h.handleError(w, r, err)
			return
		}
		config, format, err := image.DecodeConfig(file)
		if err != nil || (format != "jpeg" && format != "png") {
			h.writeError(w, 422, "invalid_image", "Envie uma imagem JPEG ou PNG válida.")
			return
		}
		if config.Width < 1 || config.Height < 1 || config.Width > domainimage.MaxDimension || config.Height > domainimage.MaxDimension || int64(config.Width)*int64(config.Height) > domainimage.MaxPixels {
			h.writeError(w, 422, "image_dimensions_exceeded", "A imagem deve ter até 8192 pixels por lado e 16 megapixels.")
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			h.handleError(w, r, err)
			return
		}
		decoded, decodedFormat, err := image.Decode(file)
		if err != nil || decodedFormat != format || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
			h.writeError(w, 422, "invalid_image", "Envie uma imagem JPEG ou PNG válida.")
			return
		}
		if format == "jpeg" {
			input.ContentType = "image/jpeg"
		} else {
			input.ContentType = "image/png"
		}
		input.Width = config.Width
		input.Height = config.Height
	}
	if len(seen) != 3 {
		h.writeError(w, 400, "invalid_image_upload", "Informe chatId, clientUploadId e uma única imagem.")
		return
	}
	result, err := h.service.Upload(r.Context(), input)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	h.writeData(w, 200, result)
}

func (h *Handler) multipartError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		h.writeError(w, 413, "image_too_large", "A imagem deve ter no máximo 10 MiB.")
		return
	}
	if r.Context().Err() != nil {
		return
	}
	h.writeError(w, 400, "invalid_image_upload", "Não foi possível ler o arquivo enviado.")
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ports.ErrImageNotFound):
		h.writeError(w, 404, "image_not_found", "A imagem não foi encontrada.")
	case errors.Is(err, ports.ErrChatNotFound):
		h.writeError(w, 404, "chat_not_found", "O chat informado não foi encontrado.")
	case errors.Is(err, ports.ErrImageUploadConflict):
		h.writeError(w, 409, "image_upload_conflict", "O identificador do upload já foi usado com outro arquivo ou conversa.")
	case errors.Is(err, ports.ErrImageUploadBusy):
		w.Header().Set("Retry-After", "2")
		h.writeError(w, 409, "image_upload_in_progress", "O upload desta imagem está em andamento. Tente novamente em instantes.")
	case errors.Is(err, ports.ErrImageExpired):
		h.writeError(w, 410, "image_upload_expired", "O upload expirou. Selecione a imagem novamente.")
	case errors.Is(err, applicationimages.ErrInvalidUpload):
		h.writeError(w, 422, "invalid_image_upload", "Os dados do upload são inválidos.")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		h.writeError(w, 504, "request_timeout", "Não foi possível concluir o upload a tempo. Tente novamente.")
	default:
		h.logger.ErrorContext(r.Context(), "image request failed", slog.Any("error", err))
		h.writeError(w, 503, "image_storage_unavailable", "Não foi possível acessar a imagem agora. Tente novamente em instantes.")
	}
}
func (h *Handler) writeError(w http.ResponseWriter, status int, code, message string) {
	if err := response.WriteError(w, status, code, message); err != nil {
		h.logger.Error("write image error", slog.Any("error", err))
	}
}
func (h *Handler) writeData(w http.ResponseWriter, status int, data any) {
	if err := response.WriteJSON(w, status, struct {
		Data any `json:"data"`
	}{data}); err != nil {
		h.logger.Error("write image response", slog.Any("error", err))
	}
}
