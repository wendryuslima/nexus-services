package chatrepository

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	chatParticipantsUniqueIndexName = "chats_participants_unique"
	chatParticipantSummaryIndexName = "chats_participant_summary_sort"
)

var _ ports.ChatRepository = (*Repository)(nil)

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

func (repository *Repository) EnsureIndexes(
	ctx context.Context,
) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "participant_one_id", Value: 1},
				{Key: "participant_two_id", Value: 1},
			},
			Options: options.Index().
				SetName(chatParticipantsUniqueIndexName).
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "participant_ids", Value: 1},
				{Key: "summary_sort_at", Value: -1},
				{Key: "_id", Value: -1},
			},
			Options: options.Index().
				SetName(chatParticipantSummaryIndexName),
		},
	}

	if _, err := repository.collection.
		Indexes().
		CreateMany(ctx, indexes); err != nil {
		return fmt.Errorf("create chat indexes: %w", err)
	}

	return nil
}
