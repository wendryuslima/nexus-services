package timelinerepository

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (repository *Repository) ListByChat(ctx context.Context, params ports.ListTimelineParams) (ports.ListTimelineResult, error) {
	if err := validateListParams(params); err != nil {
		return ports.ListTimelineResult{}, err
	}

	cursor, err := repository.collection.Find(ctx, buildListFilter(params), options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(params.Limit)+1))
	if err != nil {
		return ports.ListTimelineResult{}, fmt.Errorf("find timeline documents: %w", err)
	}
	defer cursor.Close(ctx)

	items := make([]*timeline.Item, 0, params.Limit+1)
	for cursor.Next(ctx) {
		var document timelineItemDocument
		if err := cursor.Decode(&document); err != nil {
			return ports.ListTimelineResult{}, fmt.Errorf("decode timeline document: %w", err)
		}
		item, err := document.toDomain()
		if err != nil {
			return ports.ListTimelineResult{}, fmt.Errorf("convert timeline document to domain: %w", err)
		}
		items = append(items, item)
	}
	if err := cursor.Err(); err != nil {
		return ports.ListTimelineResult{}, fmt.Errorf("iterate timeline documents: %w", err)
	}

	return buildListResult(items, params.Limit), nil
}

func validateListParams(params ports.ListTimelineParams) error {
	if params.ChatID.String() == "" {
		return ErrInvalidListChat
	}
	if params.Limit <= 0 {
		return ErrInvalidListLimit
	}
	if params.Before != nil && (params.Before.ItemID.String() == "" || params.Before.CreatedAt.IsZero()) {
		return ErrInvalidListCursor
	}
	return nil
}

func buildListFilter(params ports.ListTimelineParams) bson.D {
	filter := bson.D{{Key: "chat_id", Value: params.ChatID.String()}}
	if params.Before == nil {
		return filter
	}

	createdAt := params.Before.CreatedAt.UTC()
	return append(filter, bson.E{Key: "$or", Value: bson.A{
		bson.D{{Key: "created_at", Value: bson.D{{Key: "$lt", Value: createdAt}}}},
		bson.D{
			{Key: "created_at", Value: createdAt},
			{Key: "_id", Value: bson.D{{Key: "$lt", Value: params.Before.ItemID.String()}}},
		},
	}})
}

func buildListResult(items []*timeline.Item, limit int) ports.ListTimelineResult {
	hasNext := len(items) > limit
	if hasNext {
		items = items[:limit]
	}

	var nextCursor *ports.TimelineCursor
	if hasNext && len(items) > 0 {
		oldest := items[len(items)-1]
		nextCursor = &ports.TimelineCursor{ItemID: oldest.ID(), CreatedAt: oldest.CreatedAt()}
	}

	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}

	return ports.ListTimelineResult{Items: items, HasNext: hasNext, NextCursor: nextCursor}
}
