package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/wendryuslima/nexus-services/internal/resources/config"
)

type Server struct {
	httpServer      *http.Server
	shutdownTimeout time.Duration
	logger          *slog.Logger
}

func New(config config.HTTPConfig, handler http.Handler, logger *slog.Logger) (*Server, error) {
	if handler == nil {
		return nil, ErrNilHandler
	}

	if logger == nil {
		return nil, ErrNilLogger
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	httpServer := &http.Server{
		Addr:              config.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	return &Server{
		httpServer:      httpServer,
		shutdownTimeout: config.ShutdownTimeout,
		logger:          logger,
	}, nil
}

func (server *Server) Run(ctx context.Context) error {
	serveErrors := make(chan error, 1)

	go func() {
		server.logger.Info("HTTP server starting", slog.String("address", server.httpServer.Addr))
		serveErrors <- server.httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		return normalizeServeError(err)
	case <-ctx.Done():
		server.logger.Info("HTTP server shutdown requested")
		return server.shutdown(serveErrors)
	}
}

func (server *Server) shutdown(serveErrors <-chan error) error {
	shutdownContext, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)
	defer cancel()

	shutdownErr := server.httpServer.Shutdown(shutdownContext)
	var closeErr error
	if shutdownErr != nil {
		server.logger.Error(
			"graceful HTTP shutdown failed; forcing server close",
			slog.Any("error", shutdownErr),
		)

		closeErr = server.httpServer.Close()
	}

	serveErr := normalizeServeError(<-serveErrors)

	joinedErr := errors.Join(wrapServerError("shutdown HTTP server", shutdownErr),
		wrapServerError("force close HTTP server", closeErr),
		serveErr)
	if joinedErr != nil {
		return joinedErr
	}
	server.logger.Info("HTTP server stopped")
	return nil
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve HTTP requests: %w", err)
}

func wrapServerError(operation string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", operation, err)
}
