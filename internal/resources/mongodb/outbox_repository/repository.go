package outboxrepository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	outboxPendingScheduleIndexName  = "outbox_pending_schedule"
	outboxProcessingExpiryIndexName = "outbox_processing_expiry"
	outboxPublishedAtIndexName      = "outbox_published_at"
)

type Repository struct {
	collection *mongo.Collection
}

func NewRepository(collection *mongo.Collection) (*Repository, error) {
	if collection == nil {
		return nil, ErrNilCollection
	}
	return &Repository{
		collection: collection,
	}, nil
}

func (repository *Repository) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "next_attempt_at", Value: 1},
				{Key: "created_at", Value: 1},
				{Key: "_id", Value: 1},
			},
			Options: options.Index().SetName(outboxPendingScheduleIndexName).
				SetPartialFilterExpression(bson.D{{Key: "status", Value: statusPending}}),
		},
		{
			Keys: bson.D{
				{Key: "locked_until", Value: 1},
				{Key: "_id", Value: 1},
			},
			Options: options.Index().SetName(outboxProcessingExpiryIndexName).
				SetPartialFilterExpression(bson.D{{Key: "status", Value: statusProcessing}}),
		},
		{
			Keys: bson.D{
				{Key: "published_at", Value: 1},
			},
			Options: options.Index().SetName(outboxPublishedAtIndexName),
		},
	}
	if _, err := repository.collection.Indexes().CreateMany(ctx, indexes); err != nil {
		return fmt.Errorf("create outbox indexes: %w", err)
	}
	return nil
}
