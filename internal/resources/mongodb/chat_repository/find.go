package chatrepository

import (
	"context"
	"errors"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func (repository *Repository) FindByID(ctx context.Context, chatID chat.ID) (*chat.Chat, error) {
	if chatID.String() == "" {
		return nil, ErrInvalidChatID
	}

	var document chatDocument
	err := repository.collection.FindOne(ctx, bson.D{{Key: "_id", Value: chatID.String()}}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ports.ErrChatNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find chat document by id: %w", err)
	}

	conversation, err := document.toDomain()
	if err != nil {
		return nil, fmt.Errorf("convert chat document to domain: %w", err)
	}
	return conversation, nil
}
