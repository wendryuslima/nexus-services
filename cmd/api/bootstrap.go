package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"time"

	applicationauth "github.com/wendryuslima/nexus-services/internal/application/auth"
	applicationchats "github.com/wendryuslima/nexus-services/internal/application/chats"
	applicationtimeline "github.com/wendryuslima/nexus-services/internal/application/timeline"
	applicationusers "github.com/wendryuslima/nexus-services/internal/application/users"
	authcookie "github.com/wendryuslima/nexus-services/internal/drivers/http/auth_cookie"
	authhandler "github.com/wendryuslima/nexus-services/internal/drivers/http/handler/auth"
	chatshandler "github.com/wendryuslima/nexus-services/internal/drivers/http/handler/chats"
	usershandler "github.com/wendryuslima/nexus-services/internal/drivers/http/handler/users"
	authenticationmiddleware "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	browsersecurity "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/browser_security"
	recoverymiddleware "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/recovery"
	httprouter "github.com/wendryuslima/nexus-services/internal/drivers/http/router"
	httpserver "github.com/wendryuslima/nexus-services/internal/drivers/http/server"
	"github.com/wendryuslima/nexus-services/internal/resources/clock"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
	"github.com/wendryuslima/nexus-services/internal/resources/identifier"
	"github.com/wendryuslima/nexus-services/internal/resources/mongodb"
	chatrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/chat_repository"
	sessionrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/session_repository"
	timelinerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/timeline_repository"
	userrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/user_repository"
	"github.com/wendryuslima/nexus-services/internal/resources/security/argon2id"
	"github.com/wendryuslima/nexus-services/internal/resources/security/jwtadapter"
	"github.com/wendryuslima/nexus-services/internal/resources/security/tokendigest"
)

const (
	resourceShutdownTimeout = 10 * time.Second
	corsPreflightMaxAge     = 10 * time.Minute
	usersCollectionName     = "users"
	sessionsCollectionName  = "sessions"
	chatsCollectionName     = "chats"
	timelineCollectionName  = "timeline_items"
)

func run(ctx context.Context, logger *slog.Logger) (runErr error) {
	if logger == nil {
		return errors.New("application logger cannot be nil")
	}

	configs, err := loadConfiguration()
	if err != nil {
		return err
	}

	mongoClient, err := mongodb.Connect(ctx, configs.mongoDB)
	if err != nil {
		return fmt.Errorf("connect to MongoDB: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), resourceShutdownTimeout)
		defer cancel()
		runErr = errors.Join(runErr, mongoClient.Close(closeCtx))
	}()

	usersCollection, err := mongoClient.Collection(usersCollectionName)
	if err != nil {
		return fmt.Errorf("get users collection: %w", err)
	}
	sessionsCollection, err := mongoClient.Collection(sessionsCollectionName)
	if err != nil {
		return fmt.Errorf("get sessions collection: %w", err)
	}
	chatsCollection, err := mongoClient.Collection(chatsCollectionName)
	if err != nil {
		return fmt.Errorf(
			"get chats collection: %w",
			err,
		)
	}
	timelineCollection, err := mongoClient.Collection(timelineCollectionName)
	if err != nil {
		return fmt.Errorf("get timeline collection: %w", err)
	}

	users, err := userrepository.NewRepository(usersCollection)
	if err != nil {
		return fmt.Errorf("create user repository: %w", err)
	}
	sessions, err := sessionrepository.NewRepository(sessionsCollection)
	if err != nil {
		return fmt.Errorf("create session repository: %w", err)
	}

	chats, err := chatrepository.NewRepository(chatsCollection)
	if err != nil {
		return fmt.Errorf(
			"create chat repository: %w",
			err,
		)
	}
	timelineItems, err := timelinerepository.NewRepository(timelineCollection)
	if err != nil {
		return fmt.Errorf("create timeline repository: %w", err)
	}

	if err := users.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure user indexes: %w", err)
	}
	if err := sessions.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure session indexes: %w", err)
	}
	if err := chats.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf(
			"ensure chat indexes: %w",
			err,
		)
	}
	if err := timelineItems.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure timeline indexes: %w", err)
	}

	appClock := clock.NewSystemClock()
	ids := identifier.NewUUIDGenerator()

	passwordHasher, err := argon2id.NewHasher(argon2id.DefaultParameters())
	if err != nil {
		return fmt.Errorf("create password hasher: %w", err)
	}
	tokenManager, err := jwtadapter.NewManager(jwtadapter.Config{
		Issuer:        configs.auth.JWT.Issuer,
		Audience:      configs.auth.JWT.Audience,
		AccessSecret:  configs.auth.JWT.AccessSecret,
		RefreshSecret: configs.auth.JWT.RefreshSecret,
		Leeway:        configs.auth.JWT.Leeway,
	}, appClock)
	if err != nil {
		return fmt.Errorf("create JWT manager: %w", err)
	}
	tokenDigester := tokendigest.NewSHA256Digester()

	signupUseCase, err := applicationauth.NewSignupUseCase(users, passwordHasher, ids, appClock)
	if err != nil {
		return fmt.Errorf("create signup use case: %w", err)
	}
	signinUseCase, err := applicationauth.NewSigninUseCase(applicationauth.SigninDependencies{
		UserRepository: users, SessionRepository: sessions, PasswordHasher: passwordHasher,
		TokenManager: tokenManager, TokenDigester: tokenDigester, IDGenerator: ids, Clock: appClock,
	}, applicationauth.SigninConfig{
		AccessTokenTTL: configs.auth.AccessTokenTTL, RefreshTokenTTL: configs.auth.RefreshTokenTTL,
	})
	if err != nil {
		return fmt.Errorf("create signin use case: %w", err)
	}
	refreshUseCase, err := applicationauth.NewRefreshUseCase(applicationauth.RefreshDependencies{
		SessionRepository: sessions, TokenManager: tokenManager, TokenDigester: tokenDigester,
		IDGenerator: ids, Clock: appClock,
	}, applicationauth.RefreshConfig{
		AccessTokenTTL: configs.auth.AccessTokenTTL, RefreshTokenTTL: configs.auth.RefreshTokenTTL,
	})
	if err != nil {
		return fmt.Errorf("create refresh use case: %w", err)
	}
	logoutUseCase, err := applicationauth.NewLogoutUseCase(applicationauth.LogoutDependencies{
		SessionRepository: sessions, TokenManager: tokenManager, Clock: appClock,
	})
	if err != nil {
		return fmt.Errorf("create logout use case: %w", err)
	}

	listUsersUseCase, err := applicationusers.NewListUseCase(
		users,
	)
	if err != nil {
		return fmt.Errorf(
			"create list users use case: %w",
			err,
		)
	}
	createDirectChatUseCase, err :=
		applicationchats.NewCreateDirectUseCase(
			applicationchats.CreateDirectDependencies{
				ChatRepository: chats,
				UserRepository: users,
				IDGenerator:    ids,
				Clock:          appClock,
			},
		)
	if err != nil {
		return fmt.Errorf("create direct chat use case: %w", err)
	}
	listChatsUseCase, err := applicationchats.NewListUseCase(chats)
	if err != nil {
		return fmt.Errorf(
			"create list chats use case: %w",
			err,
		)
	}
	listTimelineUseCase, err := applicationtimeline.NewListUseCase(applicationtimeline.ListDependencies{
		ChatRepository: chats, TimelineRepository: timelineItems,
	})
	if err != nil {
		return fmt.Errorf("create list timeline use case: %w", err)
	}

	authenticateUseCase, err := applicationauth.NewAuthenticateUseCase(
		applicationauth.AuthenticateDependencies{
			SessionRepository: sessions,
			TokenManager:      tokenManager,
			Clock:             appClock,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"create authenticate use case: %w",
			err,
		)
	}

	cookies, err := authcookie.NewManager(authCookieConfig(configs.auth.Cookies), appClock)
	if err != nil {
		return fmt.Errorf("create authentication cookie manager: %w", err)
	}
	signupHandler, err := authhandler.NewSignupHandler(signupUseCase, logger)
	if err != nil {
		return fmt.Errorf("create signup handler: %w", err)
	}
	signinHandler, err := authhandler.NewSigninHandler(signinUseCase, cookies, logger)
	if err != nil {
		return fmt.Errorf("create signin handler: %w", err)
	}
	refreshHandler, err := authhandler.NewRefreshHandler(refreshUseCase, cookies, logger)
	if err != nil {
		return fmt.Errorf("create refresh handler: %w", err)
	}
	logoutHandler, err := authhandler.NewLogoutHandler(logoutUseCase, cookies, logger)
	if err != nil {
		return fmt.Errorf("create logout handler: %w", err)
	}
	listUsersHandler, err := usershandler.NewListHandler(listUsersUseCase, logger)
	if err != nil {
		return fmt.Errorf("create list users handler: %w", err)
	}
	createDirectChatHandler, err := chatshandler.NewCreateDirectHandler(createDirectChatUseCase, logger)
	if err != nil {
		return fmt.Errorf(
			"create direct chat handler: %w",
			err,
		)
	}
	listChatsHandler, err := chatshandler.NewListHandler(listChatsUseCase, logger)
	if err != nil {
		return fmt.Errorf(
			"create list chats handler: %w",
			err,
		)
	}
	timelineHandler, err := chatshandler.NewTimelineHandler(listTimelineUseCase, logger)
	if err != nil {
		return fmt.Errorf("create timeline handler: %w", err)
	}

	browserMiddleware, err := browsersecurity.New(browsersecurity.Config{
		AllowedOrigins: configs.allowedOrigins, PreflightMaxAge: corsPreflightMaxAge,
	}, logger)
	if err != nil {
		return fmt.Errorf("create browser security middleware: %w", err)
	}
	authenticationMiddleware, err := authenticationmiddleware.New(
		authenticateUseCase, cookies, logger,
	)
	if err != nil {
		return fmt.Errorf("create authentication middleware: %w", err)
	}
	router, err := httprouter.New(
		httprouter.AuthHandlers{
			Signup:  signupHandler,
			Signin:  signinHandler,
			Refresh: refreshHandler,
			Logout:  logoutHandler,
		},
		httprouter.UsersHandlers{
			List: listUsersHandler,
		},
		httprouter.ChatsHandlers{
			List:         listChatsHandler,
			CreateDirect: createDirectChatHandler,
			Timeline:     timelineHandler,
		},
		browserMiddleware,
		authenticationMiddleware,
		logger,
	)

	if err != nil {
		return fmt.Errorf(
			"create HTTP router: %w",
			err,
		)
	}
	recoveryMiddleware, err := recoverymiddleware.New(logger)
	if err != nil {
		return fmt.Errorf("create recovery middleware: %w", err)
	}
	protectedRouter, err := recoveryMiddleware.Wrap(router)
	if err != nil {
		return fmt.Errorf("apply recovery middleware: %w", err)
	}
	server, err := httpserver.New(configs.http, protectedRouter, logger)
	if err != nil {
		return fmt.Errorf("create HTTP server: %w", err)
	}

	return server.Run(ctx)
}

type applicationConfig struct {
	mongoDB        config.MongoDBConfig
	auth           config.AuthConfig
	http           config.HTTPConfig
	allowedOrigins []string
}

func loadConfiguration() (applicationConfig, error) {
	mongoConfig, err := config.LoadMongoDBConfig()
	if err != nil {
		return applicationConfig{}, fmt.Errorf("load MongoDB configuration: %w", err)
	}
	authConfig, err := config.LoadAuthConfig()
	if err != nil {
		return applicationConfig{}, fmt.Errorf("load authentication configuration: %w", err)
	}
	httpConfig, err := config.LoadHTTPConfig()
	if err != nil {
		return applicationConfig{}, fmt.Errorf("load HTTP configuration: %w", err)
	}
	allowedOrigins, err := config.LoadAllowedOrigins()
	if err != nil {
		return applicationConfig{}, fmt.Errorf(
			"load allowed origins: %w",
			err,
		)
	}

	return applicationConfig{mongoDB: mongoConfig, auth: authConfig, http: httpConfig, allowedOrigins: allowedOrigins}, nil
}

func authCookieConfig(cookieConfig config.CookieConfig) authcookie.Config {
	return authcookie.Config{
		AccessName: cookieConfig.AccessName, RefreshName: cookieConfig.RefreshName,
		Path: cookieConfig.Path, Secure: cookieConfig.Secure, SameSite: sameSiteMode(cookieConfig.SameSite),
	}
}

func sameSiteMode(value config.SameSite) http.SameSite {
	switch value {
	case config.SameSiteLax:
		return http.SameSiteLaxMode
	case config.SameSiteNone:
		return http.SameSiteNoneMode
	default:
		return http.SameSiteStrictMode
	}
}
