package outboxrepository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (repository *Repository) RecoverExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	if repository == nil || repository.collection == nil {
		return 0, ErrNilCollection
	}
	if now.IsZero() || limit <= 0 || limit > 1000 {
		return 0, errors.New("invalid outbox recovery parameters")
	}
	now = now.UTC()
	filter := bson.D{
		{Key: "status", Value: statusProcessing},
		{Key: "published_at", Value: nil},
		{Key: "locked_until", Value: bson.D{{Key: "$lte", Value: now}}},
	}
	cursor, err := repository.collection.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "locked_until", Value: 1}, {Key: "_id", Value: 1}}).
		SetLimit(int64(limit)).
		SetProjection(bson.D{{Key: "_id", Value: 1}, {Key: "lease_id", Value: 1}}))
	if err != nil {
		return 0, fmt.Errorf("find expired outbox leases: %w", err)
	}
	defer cursor.Close(ctx)
	var documents []struct {
		ID      string `bson:"_id"`
		LeaseID string `bson:"lease_id"`
	}
	if err := cursor.All(ctx, &documents); err != nil {
		return 0, fmt.Errorf("read expired outbox leases: %w", err)
	}
	if len(documents) == 0 {
		return 0, nil
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "status", Value: statusPending},
			{Key: "locked_by", Value: nil},
			{Key: "locked_until", Value: nil},
		}},
		{Key: "$unset", Value: bson.D{{Key: "lease_id", Value: ""}}},
	}
	models := make([]mongo.WriteModel, 0, len(documents))
	for _, document := range documents {
		owned := append(ownedProcessingFilter(document.ID, document.LeaseID),
			bson.E{Key: "locked_until", Value: bson.D{{Key: "$lte", Value: now}}})
		models = append(models, mongo.NewUpdateOneModel().SetFilter(owned).SetUpdate(update))
	}
	result, err := repository.collection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		return 0, fmt.Errorf("recover expired outbox leases: %w", err)
	}
	return result.ModifiedCount, nil
}
