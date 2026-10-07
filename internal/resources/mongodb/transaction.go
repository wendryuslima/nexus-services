package mongodb

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

func (client *Client) RunInTransaction(ctx context.Context, operation func(context.Context) error) error {
	if client == nil || client.client == nil {
		return ErrUninitializedClient
	}
	if operation == nil {
		return ErrNilTransactionOperation
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session, err := client.client.StartSession()
	if err != nil {
		return fmt.Errorf("start MongoDB session: %w", err)
	}
	defer func() {
		cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), client.shutdownTimeout)
		defer cancelCleanup()
		session.EndSession(cleanupContext)

	}()

	transactionOptions := options.Transaction().SetReadConcern(readconcern.Snapshot()).SetReadPreference(readpref.Primary()).SetWriteConcern(writeconcern.Majority())
	_, err = session.WithTransaction(ctx, func(transactionContext context.Context) (any, error) {
		if err := operation(transactionContext); err != nil {
			return nil, err
		}
		return nil, nil
	}, transactionOptions)
	if err != nil {
		return fmt.Errorf("execute MongoDB transaction: %w", err)
	}
	return nil
}
