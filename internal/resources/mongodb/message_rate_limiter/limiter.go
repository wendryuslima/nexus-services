package messageratelimiter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	burstLimit   = 30
	burstWindow  = 10 * time.Second
	minuteLimit  = 60
	minuteWindow = time.Minute
)

var _ ports.MessageRateLimiter = (*Limiter)(nil)

type Limiter struct {
	collection *mongo.Collection
}

func New(collection *mongo.Collection) (*Limiter, error) {
	if collection == nil {
		return nil, errors.New("message rate limiter collection cannot be nil")
	}
	return &Limiter{collection: collection}, nil
}

func (limiter *Limiter) EnsureIndexes(ctx context.Context) error {
	_, err := limiter.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetName("message_rate_limit_expiry").SetExpireAfterSeconds(0),
	})
	if err != nil {
		return fmt.Errorf("create message rate limit TTL index: %w", err)
	}
	return nil
}

func (limiter *Limiter) Reserve(ctx context.Context, userID string) (time.Duration, error) {
	if strings.TrimSpace(userID) == "" {
		return 0, errors.New("message rate limiter user ID cannot be empty")
	}
	filter := bson.D{{Key: "_id", Value: userID}}
	update := reservationPipeline()
	findOptions := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After).
		SetProjection(bson.M{"checked_at": 1, "retry_at": 1})
	var result struct {
		CheckedAt time.Time `bson:"checked_at"`
		RetryAt   time.Time `bson:"retry_at"`
	}
	err := limiter.collection.FindOneAndUpdate(ctx, filter, update, findOptions).Decode(&result)
	if mongo.IsDuplicateKeyError(err) {
		findOptions.SetUpsert(false)
		err = limiter.collection.FindOneAndUpdate(ctx, filter, update, findOptions).Decode(&result)
	}
	if err != nil {
		return 0, fmt.Errorf("reserve message rate limit: %w", err)
	}
	return result.RetryAt.Sub(result.CheckedAt), nil
}

func reservationPipeline() mongo.Pipeline {
	return mongo.Pipeline{
		bson.D{{Key: "$set", Value: bson.M{
			"checked_at": bson.M{"$max": bson.A{"$$NOW", bson.M{"$ifNull": bson.A{"$checked_at", "$$NOW"}}}},
		}}},
		bson.D{{Key: "$set", Value: bson.M{
			"timestamps": activeTimestamps(bson.M{"$ifNull": bson.A{"$timestamps", bson.A{}}}, minuteWindow),
		}}},
		bson.D{{Key: "$set", Value: bson.M{
			"burst_timestamps": activeTimestamps("$timestamps", burstWindow),
		}}},
		bson.D{{Key: "$set", Value: bson.M{
			"retry_at": bson.M{"$max": bson.A{
				windowRetryAt("$timestamps", minuteLimit, minuteWindow),
				windowRetryAt("$burst_timestamps", burstLimit, burstWindow),
			}},
		}}},
		bson.D{{Key: "$set", Value: bson.M{
			"timestamps": bson.M{"$cond": bson.A{
				bson.M{"$lte": bson.A{"$retry_at", "$checked_at"}},
				bson.M{"$concatArrays": bson.A{"$timestamps", bson.A{"$checked_at"}}},
				"$timestamps",
			}},
		}}},
		bson.D{{Key: "$set", Value: bson.M{
			"expires_at": bson.M{"$add": bson.A{bson.M{"$max": "$timestamps"}, minuteWindow.Milliseconds()}},
		}}},
		bson.D{{Key: "$unset", Value: "burst_timestamps"}},
	}
}

func activeTimestamps(input any, window time.Duration) bson.M {
	return bson.M{"$filter": bson.M{
		"input": input,
		"as":    "timestamp",
		"cond":  bson.M{"$gt": bson.A{"$$timestamp", bson.M{"$subtract": bson.A{"$checked_at", window.Milliseconds()}}}},
	}}
}

func windowRetryAt(timestamps string, limit int, window time.Duration) bson.M {
	return bson.M{"$cond": bson.A{
		bson.M{"$gte": bson.A{bson.M{"$size": timestamps}, limit}},
		bson.M{"$add": bson.A{bson.M{"$min": timestamps}, window.Milliseconds()}},
		"$checked_at",
	}}
}
