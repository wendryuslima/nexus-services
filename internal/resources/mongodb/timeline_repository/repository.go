package timelinerepository

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const timelineChatCreatedIndexName = "timeline_chat_created"

var _ ports.TimelineRepository = (*Repository)(nil)

type Repository struct {
	collection *mongo.Collection
}

func NewRepository(collection *mongo.Collection) (*Repository, error) {
	if collection == nil {
		return nil, ErrNilCollection
	}
	return &Repository{collection: collection}, nil
}

func (repository *Repository) EnsureIndexes(ctx context.Context) error {
	_, err := repository.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "chat_id", Value: 1},
			{Key: "created_at", Value: -1},
			{Key: "_id", Value: -1},
		},
		Options: options.Index().SetName(timelineChatCreatedIndexName),
	})
	if err != nil {
		return fmt.Errorf("create timeline index: %w", err)
	}
	return nil
}
