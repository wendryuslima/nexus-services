package router

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

const (
	authPrefix = "/v1/auth/"

	signupPath      = "/v1/auth/signup"
	signinPath      = "/v1/auth/signin"
	refreshPath     = "/v1/auth/refresh"
	logoutPath      = "/v1/auth/logout"
	usersPath       = "/v1/users"
	chatsPrefix     = "/v1/chats/"
	chatsPath       = "/v1/chats"
	directChatsPath = "/v1/chats/direct"
)

var _ http.Handler = (*Router)(nil)

type AuthHandlers struct {
	Signup  http.Handler
	Signin  http.Handler
	Refresh http.Handler
	Logout  http.Handler
}

type UsersHandlers struct {
	List http.Handler
}

type ChatsHandlers struct {
	List         http.Handler
	CreateDirect http.Handler
	Timeline     http.Handler
}

type BrowserSecurity interface {
	Wrap(next http.Handler) (http.Handler, error)
}

type Authentication interface {
	Wrap(next http.Handler) (http.Handler, error)
}

type Router struct {
	handler http.Handler
}

func validateChatsHandlers(handlers ChatsHandlers) error {
	if handlers.List == nil {
		return fmt.Errorf("%w: list", ErrNilChatHandler)
	}
	if handlers.CreateDirect == nil {
		return fmt.Errorf("%w: create direct", ErrNilChatHandler)
	}
	if handlers.Timeline == nil {
		return fmt.Errorf("%w: timeline", ErrNilChatHandler)
	}
	return nil
}

func New(
	authHandlers AuthHandlers,
	userHandlers UsersHandlers,
	chatHandlers ChatsHandlers,
	browserSecurity BrowserSecurity,
	authentication Authentication,
	logger *slog.Logger,
) (*Router, error) {
	if err := validateAuthHandlers(authHandlers); err != nil {
		return nil, err
	}
	if err := validateUsersHandlers(userHandlers); err != nil {
		return nil, err
	}
	if err := validateChatsHandlers(chatHandlers); err != nil {
		return nil, err
	}

	if browserSecurity == nil {
		return nil, ErrNilBrowserSecurity
	}
	if authentication == nil {
		return nil, ErrNilAuthentication
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

	authenticatedUsersHandler, err := authentication.Wrap(
		userHandlers.List,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"wrap users handler with authentication: %w",
			err,
		)
	}

	protectedUsersHandler, err := browserSecurity.Wrap(
		authenticatedUsersHandler,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"wrap users handler with browser security: %w",
			err,
		)
	}

	chatsRouter := http.NewServeMux()
	chatsRouter.Handle(chatsPath, chatHandlers.List)
	chatsRouter.Handle(directChatsPath, chatHandlers.CreateDirect)
	chatsRouter.Handle(chatsPrefix, chatHandlers.Timeline)
	chatsRouter.Handle("/", notFoundHandler)

	authenticatedChatsHandler, err := authentication.Wrap(chatsRouter)
	if err != nil {
		return nil, fmt.Errorf("wrap chats router with authentication: %w", err)
	}
	protectedChatsHandler, err := browserSecurity.Wrap(authenticatedChatsHandler)
	if err != nil {
		return nil, fmt.Errorf("wrap chats router with browser security: %w", err)
	}

	rootRouter := http.NewServeMux()

	rootRouter.Handle(usersPath, protectedUsersHandler)

	rootRouter.Handle(
		authPrefix,
		protectedAuthRouter,
	)

	rootRouter.Handle(chatsPath, protectedChatsHandler)
	rootRouter.Handle(chatsPrefix, protectedChatsHandler)

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

func validateUsersHandlers(handlers UsersHandlers) error {
	if handlers.List == nil {
		return fmt.Errorf("%w: list", ErrNilUserHandler)
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
				"A página solicitada não foi encontrada.",
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
