package mongodb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/resources/config"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const mongoDBApplicationName = "nexus-api"

type Client struct {
	client          *mongo.Client
	database        *mongo.Database
	shutdownTimeout time.Duration
}

func Connect(
	ctx context.Context,
	config config.MongoDBConfig,
) (*Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	clientOptions := options.Client().
		ApplyURI(config.URI).
		SetAppName(mongoDBApplicationName).
		SetConnectTimeout(config.ConnectTimeout).
		SetServerSelectionTimeout(config.ConnectTimeout).
		SetTimeout(config.OperationTimeout)

	mongoClient, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, fmt.Errorf(
			"create MongoDB client: %w",
			err,
		)
	}

	connectContext, cancelConnect := context.WithTimeout(
		ctx,
		config.ConnectTimeout,
	)
	defer cancelConnect()

	if err := mongoClient.Ping(
		connectContext,
		readpref.Primary(),
	); err != nil {
		pingError := fmt.Errorf(
			"ping MongoDB primary: %w",
			err,
		)

		disconnectError := disconnectAfterFailure(
			mongoClient,
			config.ShutdownTimeout,
		)

		if disconnectError != nil {
			return nil, errors.Join(
				pingError,
				disconnectError,
			)
		}

		return nil, pingError
	}

	return &Client{
		client: mongoClient,
		database: mongoClient.Database(
			config.Database,
		),
		shutdownTimeout: config.ShutdownTimeout,
	}, nil
}

func (client *Client) Collection(name string) (*mongo.Collection, error) {
	normalizedName := strings.TrimSpace(name)

	if normalizedName == "" {
		return nil, ErrInvalidCollectionName
	}
	return client.database.Collection(normalizedName), nil
}

func (client *Client) Close(ctx context.Context) error {
	if client == nil || client.client == nil {
		return nil
	}

	closeContext := ctx
	cancelClose := func() {}

	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		closeContext, cancelClose = context.WithTimeout(ctx, client.shutdownTimeout)

	}

	defer cancelClose()
	if err := client.client.Disconnect(closeContext); err != nil {
		return fmt.Errorf("disconnect MongoDB client: %w", err)
	}

	return nil

}

func disconnectAfterFailure(client *mongo.Client, timeout time.Duration) error {
	closeContext, cancelClose := context.WithTimeout(context.Background(), timeout)
	defer cancelClose()
	if err := client.Disconnect(closeContext); err != nil {
		return fmt.Errorf("disconnect MongoDB after connection failure: %w", err)
	}

	return nil
}
