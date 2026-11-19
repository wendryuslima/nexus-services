package chatrepository

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type ReadRepository struct {
	chats    *mongo.Collection
	timeline *mongo.Collection
}

func NewReadRepository(chats, items *mongo.Collection) (*ReadRepository, error) {
	if chats == nil || items == nil {
		return nil, ErrNilCollection
	}
	return &ReadRepository{chats: chats, timeline: items}, nil
}

func (r *ReadRepository) ReadStates(ctx context.Context, ids []chat.ID, participant user.ID) (map[string]ports.ChatReadState, error) {
	states := make(map[string]ports.ChatReadState, len(ids))
	if len(ids) == 0 {
		return states, nil
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = id.String()
	}
	readSequence := bson.M{"$ifNull": bson.A{bson.M{"$cond": bson.A{
		bson.M{"$eq": bson.A{"$participant_one_id", participant.String()}}, "$participant_one_read_sequence", "$participant_two_read_sequence",
	}}, int64(0)}}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"_id": bson.M{"$in": values}, "participant_ids": participant.String()}}},
		{{Key: "$set", Value: bson.M{"lastReadSequence": readSequence, "lastSequence": bson.M{"$ifNull": bson.A{"$last_sequence", int64(0)}}}}},
		{{Key: "$lookup", Value: bson.M{
			"from": r.timeline.Name(), "let": bson.M{"chat": "$_id", "read": "$lastReadSequence", "through": "$lastSequence"},
			"pipeline": mongo.Pipeline{
				{{Key: "$match", Value: bson.M{"kind": "MESSAGE", "message.sender_id": bson.M{"$ne": participant.String()}, "$expr": bson.M{"$and": bson.A{
					bson.M{"$eq": bson.A{"$chat_id", "$$chat"}}, bson.M{"$gt": bson.A{"$sequence", "$$read"}}, bson.M{"$lte": bson.A{"$sequence", "$$through"}},
				}}}}},
				{{Key: "$count", Value: "count"}},
			}, "as": "unread",
		}}},
		{{Key: "$project", Value: bson.M{"_id": 0, "chatId": "$_id", "lastSequence": 1, "lastReadSequence": 1,
			"unreadCount": bson.M{"$ifNull": bson.A{bson.M{"$arrayElemAt": bson.A{"$unread.count", 0}}, int64(0)}},
		}}},
	}
	cursor, err := r.chats.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("read chat counters: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var state ports.ChatReadState
		if err := cursor.Decode(&state); err != nil {
			return nil, err
		}
		states[state.ChatID] = state
	}
	return states, cursor.Err()
}

func (r *ReadRepository) MarkRead(ctx context.Context, id chat.ID, participant user.ID, through int64) (ports.ChatReadState, error) {
	if through < 0 || through > timeline.MaxSequence {
		return ports.ChatReadState{}, ports.ErrInvalidReadSequence
	}
	// Both participant fields are updated atomically. $max prevents a delayed tab
	// from moving the watermark backwards; $min never acknowledges future messages.
	fields := bson.M{}
	for _, slot := range []string{"one", "two"} {
		field := "participant_" + slot + "_read_sequence"
		previous := bson.M{"$ifNull": bson.A{"$" + field, int64(0)}}
		fields[field] = bson.M{"$cond": bson.A{
			bson.M{"$eq": bson.A{"$participant_" + slot + "_id", participant.String()}},
			bson.M{"$max": bson.A{previous, bson.M{"$min": bson.A{through, bson.M{"$ifNull": bson.A{"$last_sequence", int64(0)}}}}}}, previous,
		}}
	}
	result, err := r.chats.UpdateOne(ctx, bson.M{"_id": id.String(), "participant_ids": participant.String()}, mongo.Pipeline{{{Key: "$set", Value: fields}}})
	if err != nil {
		return ports.ChatReadState{}, fmt.Errorf("mark chat read: %w", err)
	}
	if result.MatchedCount == 0 {
		return ports.ChatReadState{}, ports.ErrChatNotFound
	}
	states, err := r.ReadStates(ctx, []chat.ID{id}, participant)
	if err != nil {
		return ports.ChatReadState{}, err
	}
	state, ok := states[id.String()]
	if !ok {
		return ports.ChatReadState{}, ports.ErrChatNotFound
	}
	return state, nil
}
