package timelinerepository

import (
	"context"
	"slices"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (repository *Repository) readAround(ctx context.Context, params ports.ListTimelineParams, upper int64, result *ports.ListTimelineResult) error {
	anchor := *params.AroundSequence
	// pageSize is the maximum total window, including the anchor.
	beforeLimit := (params.Limit-1)/2 + 1
	afterLimit := params.Limit - beforeLimit
	read := func(bounds bson.M, order int, limit int) ([]*timeline.Item, bool, error) {
		cursor, err := repository.collection.Find(ctx, bson.M{"chat_id": params.ChatID.String(), "sequence": bounds},
			options.Find().SetSort(bson.D{{Key: "sequence", Value: order}}).SetLimit(int64(limit+1)))
		if err != nil {
			return nil, false, err
		}
		defer cursor.Close(ctx)
		items := make([]*timeline.Item, 0, limit+1)
		for cursor.Next(ctx) {
			var document timelineItemDocument
			if err := cursor.Decode(&document); err != nil {
				return nil, false, err
			}
			item, err := document.toDomain()
			if err != nil {
				return nil, false, err
			}
			items = append(items, item)
		}
		if err := cursor.Err(); err != nil {
			return nil, false, err
		}
		more := len(items) > limit
		if more {
			items = items[:limit]
		}
		return items, more, nil
	}
	before, hasOlder, err := read(bson.M{"$lte": anchor}, -1, beforeLimit)
	if err != nil {
		return err
	}
	if anchor > upper || len(before) == 0 || before[0].Sequence().Int64() != anchor {
		return ports.ErrInvalidTimelinePagination
	}
	after, hasNewer, err := read(bson.M{"$gt": anchor, "$lte": upper}, 1, afterLimit)
	if err != nil {
		return err
	}
	slices.Reverse(before)
	result.Items = append(before, after...)
	result.HasOlder, result.HasNewer, result.HasNext = hasOlder, hasNewer, hasOlder
	if hasOlder {
		value := result.Items[0].Sequence().Int64()
		result.NextBeforeSequence = &value
	}
	if hasNewer {
		value := result.Items[len(result.Items)-1].Sequence().Int64()
		result.NextAfterSequence = &value
	}
	return nil
}
