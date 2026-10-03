package outboxrepository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	outboxStatusScheduleIndexName = "outbox_status_schedule"
	outboxLockedUntilIndexName    = "outbox_locked_until"
	outboxPublishedAtIndexName    = "outbox_published_at"
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
				{Key: "status", Value: 1},
				{Key: "next_attempt_at", Value: 1},
				{Key: "created_at", Value: 1},
			},
			Options: options.Index().SetName(outboxStatusScheduleIndexName),
		},
		{
			Keys: bson.D{
				{Key: "locked_until", Value: 1},
			},
			Options: options.Index().SetName(outboxLockedUntilIndexName),
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
