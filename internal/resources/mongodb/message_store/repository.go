package messagestore

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

type TransactionRunner interface {
	RunInTransaction(ctx context.Context, operation func(context.Context) error) error
}

type Repository struct {
	transactions       TransactionRunner
	chatCollection     *mongo.Collection
	timelineCollection *mongo.Collection
	outboxCollection   *mongo.Collection
}

func NewRepository(transactions TransactionRunner, chatsCollection *mongo.Collection, timelineCollection *mongo.Collection, outboxCollection *mongo.Collection) (*Repository, error) {
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
	return &Repository{
		transactions:       transactions,
		chatCollection:     chatsCollection,
		timelineCollection: timelineCollection,
		outboxCollection:   outboxCollection,
	}, nil
}
