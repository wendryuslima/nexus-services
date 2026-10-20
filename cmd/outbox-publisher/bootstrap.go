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
	applicationoutbox "github.com/wendryuslima/nexus-services/internal/application/outbox"

	outboxrunner "github.com/wendryuslima/nexus-services/internal/drivers/worker/outbox_runner"
	"github.com/wendryuslima/nexus-services/internal/resources/clock"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
	"github.com/wendryuslima/nexus-services/internal/resources/identifier"
	"github.com/wendryuslima/nexus-services/internal/resources/mongodb"
	outboxrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/outbox_repository"
	jetstreampublisher "github.com/wendryuslima/nexus-services/internal/resources/nats/jetstream_publisher"
)

const (
	outboxCollectionName = "outbox_events"

	partitionCount = 1
	leaseDuration  = 30 * time.Second
	publishTimeout = 10 * time.Second
	finishTimeout  = 5 * time.Second
	idleInterval   = time.Second
	errorInterval  = 2 * time.Second
)

func run(ctx context.Context, logger *slog.Logger) (runErr error) {
	if logger == nil {
		return errors.New("outbox logger cannot be nil")
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
	collection, err := mongoClient.Collection(outboxCollectionName)
	if err != nil {
		return fmt.Errorf("get outbox collection: %w", err)
	}
	store, err := outboxrepository.NewRepository(collection)
	if err != nil {
		return fmt.Errorf("create outbox repository: %w", err)
	}
	if err := store.EnsureIndexes(ctx); err != nil {
		return fmt.Errorf("ensure outbox indexes: %w", err)
	}

	nc, err := nats.Connect(natsURL, nats.Name("nexus-outbox-publisher"), nats.Timeout(5*time.Second), nats.MaxReconnects(-1), nats.ReconnectWait(2*time.Second))
	if err != nil {
		return fmt.Errorf("connect to NATS: %w", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("create JetStream client: %w", err)
	}
	publisher, err := jetstreampublisher.NewPublisher(js, partitionCount)
	if err != nil {
		return fmt.Errorf("create JetStream publisher: %w", err)
	}

	ids := identifier.NewUUIDGenerator()
	instanceID, err := ids.New(ctx)
	if err != nil {
		return fmt.Errorf("generate worker instance ID: %w", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read worker hostname: %w", err)
	}

	workerID := hostname + ":" + instanceID
	step, err := applicationoutbox.NewPublishOne(
		applicationoutbox.PublishOneDependencies{
			Store:          store,
			Publisher:      publisher,
			IDGenerator:    ids,
			Clock:          clock.NewSystemClock(),
			WorkerID:       workerID,
			LeaseDuration:  leaseDuration,
			PublishTimeout: publishTimeout,
			FinishTimeout:  finishTimeout,
		},
	)
	if err != nil {
		return fmt.Errorf("create outbox publication step: %w", err)
	}

	runner, err := outboxrunner.NewRunner(
		step,
		logger,
		idleInterval,
		errorInterval,
	)
	if err != nil {
		return fmt.Errorf("create outbox runner: %w", err)
	}

	logger.Info("outbox publisher started", slog.String("worker_id", workerID))
	return runner.Run(ctx)
}
