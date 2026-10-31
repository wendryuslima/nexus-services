package main

import (
	"context"
	"errors"
	"fmt"
	websocketgateway "github.com/wendryuslima/nexus-services/internal/drivers/websocket"
	realtimesubscriber "github.com/wendryuslima/nexus-services/internal/resources/nats/realtime_subscriber"
	"log/slog"
	"net/http"

	"time"

	applicationauth "github.com/wendryuslima/nexus-services/internal/application/auth"
	applicationchats "github.com/wendryuslima/nexus-services/internal/application/chats"
	applicationimages "github.com/wendryuslima/nexus-services/internal/application/images"
	applicationmessages "github.com/wendryuslima/nexus-services/internal/application/messages"
	applicationtimeline "github.com/wendryuslima/nexus-services/internal/application/timeline"
	applicationusers "github.com/wendryuslima/nexus-services/internal/application/users"
	authcookie "github.com/wendryuslima/nexus-services/internal/drivers/http/auth_cookie"
	authhandler "github.com/wendryuslima/nexus-services/internal/drivers/http/handler/auth"
	chatshandler "github.com/wendryuslima/nexus-services/internal/drivers/http/handler/chats"
	imageshandler "github.com/wendryuslima/nexus-services/internal/drivers/http/handler/images"
	usershandler "github.com/wendryuslima/nexus-services/internal/drivers/http/handler/users"
	authenticationmiddleware "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	browsersecurity "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/browser_security"
	recoverymiddleware "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/recovery"
	httprouter "github.com/wendryuslima/nexus-services/internal/drivers/http/router"
	httpserver "github.com/wendryuslima/nexus-services/internal/drivers/http/server"
	imagecleanup "github.com/wendryuslima/nexus-services/internal/drivers/worker/image_cleanup"
	"github.com/wendryuslima/nexus-services/internal/resources/clock"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
	"github.com/wendryuslima/nexus-services/internal/resources/garage"
	"github.com/wendryuslima/nexus-services/internal/resources/identifier"
	"github.com/wendryuslima/nexus-services/internal/resources/mongodb"
	chatrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/chat_repository"
	imagerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/image_repository"
	messageratelimiter "github.com/wendryuslima/nexus-services/internal/resources/mongodb/message_rate_limiter"
	messagestore "github.com/wendryuslima/nexus-services/internal/resources/mongodb/message_store"
	outboxrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/outbox_repository"
	sessionrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/session_repository"
	timelinerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/timeline_repository"
	userrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/user_repository"
	"github.com/wendryuslima/nexus-services/internal/resources/security/argon2id"
	"github.com/wendryuslima/nexus-services/internal/resources/security/jwtadapter"
	"github.com/wendryuslima/nexus-services/internal/resources/security/tokendigest"
)

const (
	resourceShutdownTimeout         = 10 * time.Second
	corsPreflightMaxAge             = 10 * time.Minute
	usersCollectionName             = "users"
	sessionsCollectionName          = "sessions"
	chatsCollectionName             = "chats"
	timelineCollectionName          = "timeline_items"
	outboxCollectionName            = "outbox_events"
	messageRateLimitsCollectionName = "message_rate_limits"
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
	outboxCollection, err := mongoClient.Collection(outboxCollectionName)
	if err != nil {
		return fmt.Errorf("get outbox collection: %w", err)
	}
	messageRateLimitsCollection, err := mongoClient.Collection(messageRateLimitsCollectionName)
	if err != nil {
		return fmt.Errorf("get message rate limits collection: %w", err)
	}
	imagesCollection, err := mongoClient.Collection("images")
	if err != nil {
		return err
	}
	images, err := imagerepository.New(imagesCollection)
	if err != nil {
		return err
	}
	if err := images.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure image indexes: %w", err)
	}
	imageStorage, err := garage.New(configs.images)
	if err != nil {
		return fmt.Errorf("create Garage storage: %w", err)
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
	outboxEvents, err := outboxrepository.NewRepository(outboxCollection)
	if err != nil {
		return fmt.Errorf("create outbox repository: %w", err)
	}
	messageStore, err := messagestore.NewRepository(
		mongoClient,
		chatsCollection,
		timelineCollection,
		outboxCollection,
		imagesCollection,
	)
	if err != nil {
		return fmt.Errorf("create message store: %w", err)
	}
	messageRateLimiter, err := messageratelimiter.New(messageRateLimitsCollection)
	if err != nil {
		return fmt.Errorf("create message rate limiter: %w", err)
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
	if err := outboxEvents.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure outbox indexes: %w", err)
	}
	if err := messageRateLimiter.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure message rate limit indexes: %w", err)
	}

	appClock := clock.NewSystemClock()
	ids := identifier.NewUUIDGenerator()
	imageService, err := applicationimages.New(images, imageStorage, chats, ids, appClock, configs.images.Bucket)
	if err != nil {
		return err
	}
	imagesHandler, err := imageshandler.New(imageService, messageRateLimiter, logger)
	if err != nil {
		return err
	}

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
	listChatsUseCase, err := applicationchats.NewListUseCase(chats, users)
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
	createMessageUseCase, err := applicationmessages.NewCreateUseCase(
		applicationmessages.CreateDependencies{
			ChatRepository: chats,
			MessageStore:   messageStore,
			IDGenerator:    ids,
			Clock:          appClock,
		},
	)
	if err != nil {
		return fmt.Errorf("create message use case: %w", err)
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
	sessionUseCase, err := applicationauth.NewSessionUseCase(users, appClock)
	if err != nil {
		return fmt.Errorf("create session use case: %w", err)
	}
	sessionHandler, err := authhandler.NewSessionHandler(sessionUseCase, logger)
	if err != nil {
		return err
	}
	if err := timelineItems.CheckRealtimeData(ctx); err != nil {
		return fmt.Errorf("realtime data audit: %w", err)
	}
	subscriber, err := realtimesubscriber.New(configs.realtime.NATSURL, configs.realtime.MaxMessageBytes, configs.realtime.NATSFlushTimeout, logger)
	if err != nil {
		return fmt.Errorf("create realtime subscriber: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), configs.realtime.ShutdownTimeout)
		defer cancel()
		runErr = errors.Join(runErr, subscriber.Close(closeCtx))
	}()
	gateway, err := websocketgateway.New(configs.realtime, configs.allowedOrigins, subscriber, sessions, ids, logger)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), configs.realtime.ShutdownTimeout)
		defer cancel()
		runErr = errors.Join(runErr, gateway.Shutdown(closeCtx))
	}()
	logger.Info("realtime gateway started", "path", "/v1/realtime")
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
	createMessageHandler, err := chatshandler.NewCreateMessageHandler(
		createMessageUseCase,
		messageRateLimiter,
		logger,
	)
	if err != nil {
		return fmt.Errorf("create message handler: %w", err)
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
			Session: sessionHandler,
			Signup:  signupHandler,
			Signin:  signinHandler,
			Refresh: refreshHandler,
			Logout:  logoutHandler,
		},
		httprouter.UsersHandlers{
			List: listUsersHandler,
		},
		httprouter.ChatsHandlers{
			List:          listChatsHandler,
			CreateDirect:  createDirectChatHandler,
			CreateMessage: createMessageHandler,
			Timeline:      timelineHandler,
		},
		imagesHandler,
		browserMiddleware,
		authenticationMiddleware,
		logger,
		gateway,
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

	cleanupCtx, cancelCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() { defer close(cleanupDone); imagecleanup.Run(cleanupCtx, imageService, logger) }()
	defer func() { cancelCleanup(); <-cleanupDone }()
	serverCtx, cancelServer := context.WithCancel(context.Background())
	defer cancelServer()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(serverCtx) }()
	select {
	case err := <-serverDone:
		return err
	case <-ctx.Done():
		var realtimeShutdownErr error
		if gateway != nil {
			shutdown, cancel := context.WithTimeout(context.Background(), configs.realtime.ShutdownTimeout)
			err := gateway.Shutdown(shutdown)
			realtimeShutdownErr = err
			cancel()
			if err != nil {
				logger.Warn("realtime shutdown deadline reached")
			}
		}
		cancelServer()
		return errors.Join(realtimeShutdownErr, <-serverDone)
	}
}

type applicationConfig struct {
	images         config.ImagesConfig
	realtime       config.RealtimeConfig
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

	realtimeConfig, err := config.LoadRealtimeConfig()
	if err != nil {
		return applicationConfig{}, err
	}
	imagesConfig, err := config.LoadImagesConfig()
	if err != nil {
		return applicationConfig{}, fmt.Errorf("load image configuration: %w", err)
	}
	return applicationConfig{images: imagesConfig, realtime: realtimeConfig, mongoDB: mongoConfig, auth: authConfig, http: httpConfig, allowedOrigins: allowedOrigins}, nil
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
