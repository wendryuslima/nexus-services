package router

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

const (
	authPrefix = "/v1/auth/"

	signupPath  = "/v1/auth/signup"
	signinPath  = "/v1/auth/signin"
	refreshPath = "/v1/auth/refresh"
	logoutPath  = "/v1/auth/logout"
)

var _ http.Handler = (*Router)(nil)

type AuthHandlers struct {
	Signup  http.Handler
	Signin  http.Handler
	Refresh http.Handler
	Logout  http.Handler
}

type BrowserSecurity interface {
	Wrap(next http.Handler) (http.Handler, error)
}

type Router struct {
	handler http.Handler
}

func New(
	authHandlers AuthHandlers,
	browserSecurity BrowserSecurity,
	logger *slog.Logger,
) (*Router, error) {
	if err := validateAuthHandlers(authHandlers); err != nil {
		return nil, err
	}

	if browserSecurity == nil {
		return nil, ErrNilBrowserSecurity
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	notFoundHandler := newNotFoundHandler(logger)

	authRouter := http.NewServeMux()

	authRouter.Handle(
		signupPath,
		authHandlers.Signup,
	)
	authRouter.Handle(
		signinPath,
		authHandlers.Signin,
	)
	authRouter.Handle(
		refreshPath,
		authHandlers.Refresh,
	)
	authRouter.Handle(
		logoutPath,
		authHandlers.Logout,
	)

	authRouter.Handle("/", notFoundHandler)

	protectedAuthRouter, err := browserSecurity.Wrap(
		authRouter,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"wrap authentication router with browser security: %w",
			err,
		)
	}

	rootRouter := http.NewServeMux()

	rootRouter.Handle(
		authPrefix,
		protectedAuthRouter,
	)

	rootRouter.Handle(
		"/v1/auth",
		notFoundHandler,
	)

	rootRouter.Handle(
		"/",
		notFoundHandler,
	)

	return &Router{
		handler: rootRouter,
	}, nil
}

func (router *Router) ServeHTTP(
	writer http.ResponseWriter,
	httpRequest *http.Request,
) {
	router.handler.ServeHTTP(
		writer,
		httpRequest,
	)
}

func validateAuthHandlers(
	handlers AuthHandlers,
) error {
	if handlers.Signup == nil {
		return fmt.Errorf(
			"%w: signup",
			ErrNilAuthHandler,
		)
	}

	if handlers.Signin == nil {
		return fmt.Errorf(
			"%w: signin",
			ErrNilAuthHandler,
		)
	}

	if handlers.Refresh == nil {
		return fmt.Errorf(
			"%w: refresh",
			ErrNilAuthHandler,
		)
	}

	if handlers.Logout == nil {
		return fmt.Errorf(
			"%w: logout",
			ErrNilAuthHandler,
		)
	}

	return nil
}

func newNotFoundHandler(
	logger *slog.Logger,
) http.Handler {
	return http.HandlerFunc(
		func(
			writer http.ResponseWriter,
			httpRequest *http.Request,
		) {
			if err := response.WriteError(
				writer,
				http.StatusNotFound,
				"route_not_found",
				"The requested route does not exist.",
			); err != nil {
				logger.ErrorContext(
					httpRequest.Context(),
					"failed to write route not found response",
					slog.Any("error", err),
					slog.String(
						"method",
						httpRequest.Method,
					),
					slog.String(
						"path",
						httpRequest.URL.Path,
					),
				)
			}
		},
	)
}
