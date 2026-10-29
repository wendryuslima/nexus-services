package timelinerepository

import (
	"context"
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func (repository *Repository) CheckRealtimeData(ctx context.Context) error {
	session, err := repository.collection.Database().Client().StartSession()
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		session.EndSession(cleanup)
	}()
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) { return nil, repository.checkRealtimeSnapshot(tx) }, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetReadPreference(readpref.Primary()))
	repository.sequenceDataChecked.Store(err == nil)
	return err
}

func (repository *Repository) checkRealtimeSnapshot(ctx context.Context) error {
	valid := bson.M{"$and": bson.A{
		bson.M{"$in": bson.A{bson.M{"$type": "$sequence"}, bson.A{"int", "long"}}},
		bson.M{"$gt": bson.A{"$sequence", 0}},
		bson.M{"$lte": bson.A{"$sequence", timeline.MaxSequence}},
		bson.M{"$eq": bson.A{bson.M{"$type": "$message.client_message_id"}, "string"}},
		bson.M{"$ne": bson.A{"$message.client_message_id", ""}},
	}}
	inconsistent := bson.M{"$or": bson.A{
		bson.M{"$ne": bson.A{"$min", 1}},
		bson.M{"$ne": bson.A{"$max", "$count"}},
		bson.M{"$ne": bson.A{"$valid", "$count"}},
		bson.M{"$ne": bson.A{bson.M{"$size": "$chat"}, 1}},
		bson.M{"$ne": bson.A{bson.M{"$arrayElemAt": bson.A{"$chat.last_sequence", 0}}, "$max"}},
	}}
	pipeline := bson.A{
		bson.M{"$group": bson.M{
			"_id": "$chat_id", "count": bson.M{"$sum": 1},
			"min": bson.M{"$min": "$sequence"}, "max": bson.M{"$max": "$sequence"},
			"valid": bson.M{"$sum": bson.M{"$cond": bson.A{valid, 1, 0}}},
		}},
		bson.M{"$lookup": bson.M{"from": "chats", "localField": "_id", "foreignField": "_id", "as": "chat"}},
		bson.M{"$match": bson.M{"$expr": inconsistent}},
		bson.M{"$limit": 1},
	}
	cursor, err := repository.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	if cursor.Next(ctx) {
		return fmt.Errorf("legacy or non-contiguous timeline detected; follow docs/realtime-operations.md before starting the API")
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	chatCounters := bson.A{
		bson.M{"$lookup": bson.M{"from": repository.collection.Name(), "let": bson.M{"chat": "$_id"}, "pipeline": bson.A{
			bson.M{"$match": bson.M{"$expr": bson.M{"$eq": bson.A{"$chat_id", "$$chat"}}}},
			bson.M{"$sort": bson.M{"sequence": -1}}, bson.M{"$limit": 1}, bson.M{"$project": bson.M{"sequence": 1}},
		}, "as": "tail"}},
		bson.M{"$match": bson.M{"$expr": bson.M{"$ne": bson.A{
			bson.M{"$ifNull": bson.A{"$last_sequence", 0}},
			bson.M{"$ifNull": bson.A{bson.M{"$arrayElemAt": bson.A{"$tail.sequence", 0}}, 0}},
		}}}},
		bson.M{"$limit": 1},
	}
	chats, err := repository.collection.Database().Collection("chats").Aggregate(ctx, chatCounters)
	if err != nil {
		return err
	}
	defer chats.Close(ctx)
	if chats.Next(ctx) {
		return fmt.Errorf("chat sequence counter does not match its persisted tail")
	}
	return chats.Err()
}
