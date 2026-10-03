package timelinerepository

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	timelineChatCreatedIndexName = "timeline_chat_created"

	timelineChatSequenceUniqueIndexName = "timeline_chat_sequence_unique"

	timelineSenderClientMessageUniqueIndexName = "timeline_sender_client_message_unique"
)

var _ ports.TimelineRepository = (*Repository)(nil)

type Repository struct {
	collection *mongo.Collection
}

func NewRepository(
	collection *mongo.Collection,
) (*Repository, error) {
	if collection == nil {
		return nil, ErrNilCollection
	}

	return &Repository{
		collection: collection,
	}, nil
}

func (repository *Repository) EnsureIndexes(
	ctx context.Context,
) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "chat_id", Value: 1},
				{Key: "created_at", Value: -1},
				{Key: "_id", Value: -1},
			},
			Options: options.Index().
				SetName(timelineChatCreatedIndexName),
		},
		{
			Keys: bson.D{
				{Key: "chat_id", Value: 1},
				{Key: "sequence", Value: 1},
			},
			Options: options.Index().
				SetName(timelineChatSequenceUniqueIndexName).
				SetUnique(true).
				SetPartialFilterExpression(bson.D{
					{
						Key: "sequence",
						Value: bson.D{
							{Key: "$exists", Value: true},
						},
					},
				}),
		},
		{
			Keys: bson.D{
				{Key: "message.sender_id", Value: 1},
				{Key: "message.client_message_id", Value: 1},
			},
			Options: options.Index().
				SetName(timelineSenderClientMessageUniqueIndexName).
				SetUnique(true).
				SetPartialFilterExpression(bson.D{
					{
						Key: "message.client_message_id",
						Value: bson.D{
							{Key: "$exists", Value: true},
						},
					},
				}),
		},
	}

	if _, err := repository.collection.
		Indexes().
		CreateMany(ctx, indexes); err != nil {
		return fmt.Errorf("create timeline indexes: %w", err)
	}

	return nil
}
