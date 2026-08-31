package recovery

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

type Middleware struct {
	logger *slog.Logger
}

func New(logger *slog.Logger) (*Middleware, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	return &Middleware{
		logger: logger,
	}, nil
}

func (middleware *Middleware) Wrap(next http.Handler) (http.Handler, error) {
	if next == nil {
		return nil, ErrNilHandler
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		trackedWriter := &trackingResponseWriter{
			ResponseWriter: writer,
		}

		defer func() {
			recoveredValue := recover()
			if recoveredValue == nil {
				return
			}

			middleware.logger.ErrorContext(
				request.Context(),
				"panic recovered while handling HTTP request",
				slog.Any("panic", recoveredValue),
				slog.String("method", request.Method),
				slog.String("path", request.URL.Path),
				slog.String("stack", string(debug.Stack())),
			)

			if trackedWriter.wroteHeader {
				return
			}

			err := response.WriteError(
				trackedWriter,
				http.StatusInternalServerError,
				"internal_server_error",
				"an unexpected error occurred",
			)
			if err != nil {
				middleware.logger.ErrorContext(
					request.Context(),
					"failed to write panic response",
					slog.Any("error", err),
				)
			}
		}()

		next.ServeHTTP(trackedWriter, request)
	}), nil
}

type trackingResponseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (writer *trackingResponseWriter) WriteHeader(statusCode int) {
	if writer.wroteHeader {
		return
	}

	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(statusCode)
}

func (writer *trackingResponseWriter) Write(content []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}

	return writer.ResponseWriter.Write(content)
}

func (writer *trackingResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}
