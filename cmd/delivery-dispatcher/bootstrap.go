package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/wendryuslima/nexus-services/internal/application/delivery"
	deliveryrunner "github.com/wendryuslima/nexus-services/internal/drivers/worker/delivery_runner"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
	"github.com/wendryuslima/nexus-services/internal/resources/mongodb"
	chatrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/chat_repository"
	deliveryfailure "github.com/wendryuslima/nexus-services/internal/resources/nats/delivery_failure"
	realtimepublisher "github.com/wendryuslima/nexus-services/internal/resources/nats/realtime_publisher"
)

const (
	chatCollectionName = "chats"
	streamName         = "CHAT_EVENTS"
	consumerName       = "MESSAGE_DELIVERY"
	consumerFilter     = "chat.message.created.>"
)

func run(ctx context.Context, logger *slog.Logger) (runErr error) {
	if logger == nil {
		return errors.New("delivery logger cannot be nil")
	}
	mongoConfig, err := config.LoadMongoDBConfig()
	if err != nil {
		return fmt.Errorf("load MongoDB configuration: %w", err)
	}
	natsURL := strings.TrimSpace(os.Getenv("NATS_URL"))
	if natsURL == "" {
		return errors.New("NATS_URL is required")
	}

	mongoClient, err := mongodb.Connect(ctx, mongoConfig)
	if err != nil {
		return fmt.Errorf("connect to MongoDB: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), mongoConfig.ShutdownTimeout)
		defer cancel()
		runErr = errors.Join(runErr, mongoClient.Close(closeCtx))
	}()
	chatCollection, err := mongoClient.Collection(chatCollectionName)
	if err != nil {
		return fmt.Errorf("get chat collection: %w", err)
	}
	chats, err := chatrepository.NewRepository(chatCollection)
	if err != nil {
		return fmt.Errorf("create chat repository: %w", err)
	}

	nc, err := nats.Connect(natsURL,
		nats.Name("nexus-delivery-dispatcher"),
		nats.Timeout(5*time.Second),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("create JetStream client: %w", err)
	}

	provisionCtx, cancelProvision := context.WithTimeout(ctx, 10*time.Second)
	consumer, err := js.CreateOrUpdateConsumer(provisionCtx, streamName, jetstream.ConsumerConfig{
		Durable:       consumerName,
		FilterSubject: consumerFilter,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       60 * time.Second,
		MaxAckPending: 32,
		MaxDeliver:    -1,
	})
	cancelProvision()
	if err != nil {
		return fmt.Errorf("provision durable consumer: %w", err)
	}

	realtime, err := realtimepublisher.NewPublisher(nc)
	if err != nil {
		return fmt.Errorf("create realtime publisher: %w", err)
	}
	failures, err := deliveryfailure.NewPublisher(js)
	if err != nil {
		return fmt.Errorf("create delivery failure publisher: %w", err)
	}
	handler, err := delivery.NewDispatchMessageCreated(chats, realtime)
	if err != nil {
		return fmt.Errorf("create delivery use case: %w", err)
	}
	runner, err := deliveryrunner.NewRunner(consumer, handler, failures, logger)
	if err != nil {
		return fmt.Errorf("create delivery runner: %w", err)
	}
	logger.Info("delivery dispatcher started", slog.String("stream", streamName), slog.String("consumer", consumerName))
	return runner.Run(ctx)
}
