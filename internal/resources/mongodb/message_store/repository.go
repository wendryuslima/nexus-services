package messagestore

import (
	"context"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type TransactionRunner interface {
	RunInTransaction(
		ctx context.Context,
		operation func(context.Context) error,
	) error
}

var _ ports.MessageStore = (*Repository)(nil)

type Repository struct {
	transactions       TransactionRunner
	chatCollection     *mongo.Collection
	timelineCollection *mongo.Collection
	outboxCollection   *mongo.Collection
	imagesCollection   *mongo.Collection
}

func NewRepository(
	transactions TransactionRunner,
	chatsCollection *mongo.Collection,
	timelineCollection *mongo.Collection,
	outboxCollection *mongo.Collection,
	imagesCollection ...*mongo.Collection,
) (*Repository, error) {
	if transactions == nil {
		return nil, ErrNilTransactionRunner
	}

	if chatsCollection == nil {
		return nil, ErrNilChatsCollection
	}

	if timelineCollection == nil {
		return nil, ErrNilTimelineCollection
	}

	if outboxCollection == nil {
		return nil, ErrNilOutboxCollection
	}

	var images *mongo.Collection
	if len(imagesCollection) > 0 {
		images = imagesCollection[0]
	}
	return &Repository{
		transactions:       transactions,
		chatCollection:     chatsCollection,
		timelineCollection: timelineCollection,
		outboxCollection:   outboxCollection,
		imagesCollection:   images,
	}, nil
}
