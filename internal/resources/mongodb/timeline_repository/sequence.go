package timelinerepository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func (repository *Repository) listBySequence(ctx context.Context, params ports.ListTimelineParams) (ports.ListTimelineResult, error) {
	session, err := repository.collection.Database().Client().StartSession()
	if err != nil {
		return ports.ListTimelineResult{}, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		session.EndSession(cleanup)
	}()
	var result ports.ListTimelineResult
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		result = ports.ListTimelineResult{}
		filter := bson.D{{Key: "chat_id", Value: params.ChatID.String()}}
		invalid := append(slices.Clone(filter), bson.E{Key: "$or", Value: bson.A{
			bson.D{{Key: "sequence", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{Key: "sequence", Value: bson.D{{Key: "$lte", Value: 0}}}},
			bson.D{{Key: "sequence", Value: bson.D{{Key: "$gt", Value: timeline.MaxSequence}}}},
			bson.D{{Key: "message.client_message_id", Value: bson.D{{Key: "$exists", Value: false}}}},
		}})
		if !repository.sequenceDataChecked.Load() {
			if count, err := repository.collection.CountDocuments(tx, invalid, options.Count().SetLimit(1)); err != nil {
				return nil, err
			} else if count != 0 {
				return nil, fmt.Errorf("%w: legacy timeline requires migration", ErrInvalidDocument)
			}
		}
		var tail struct {
			Sequence int64 `bson:"sequence"`
		}
		err := repository.collection.FindOne(tx, filter, options.FindOne().SetSort(bson.D{{Key: "sequence", Value: -1}}).SetProjection(bson.D{{Key: "sequence", Value: 1}})).Decode(&tail)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
		upper := tail.Sequence
		if params.ThroughSequence != nil {
			if *params.ThroughSequence > upper {
				return nil, ports.ErrInvalidTimelinePagination
			}
			upper = *params.ThroughSequence
		}
		if params.AfterSequence != nil && *params.AfterSequence > upper {
			return nil, ports.ErrInvalidTimelinePagination
		}
		result.SnapshotThroughSequence = &upper
		bounds := bson.D{{Key: "$lte", Value: upper}}
		order := int32(-1)
		if params.AfterSequence != nil {
			bounds = append(bounds, bson.E{Key: "$gt", Value: *params.AfterSequence})
			order = 1
		}
		if params.BeforeSequence != nil {
			bounds = append(bounds, bson.E{Key: "$lt", Value: *params.BeforeSequence})
		}
		filter = append(filter, bson.E{Key: "sequence", Value: bounds})
		cursor, err := repository.collection.Find(tx, filter, options.Find().SetSort(bson.D{{Key: "sequence", Value: order}}).SetLimit(int64(params.Limit)+1))
		if err != nil {
			return nil, err
		}
		defer cursor.Close(tx)
		items := make([]*timeline.Item, 0, params.Limit+1)
		for cursor.Next(tx) {
			var document timelineItemDocument
			if err := cursor.Decode(&document); err != nil {
				return nil, err
			}
			item, err := document.toDomain()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if err := cursor.Err(); err != nil {
			return nil, err
		}
		result.HasNext = len(items) > params.Limit
		if result.HasNext {
			items = items[:params.Limit]
		}
		if params.AfterSequence != nil {
			if result.HasNext && len(items) > 0 {
				next := items[len(items)-1].Sequence().Int64()
				result.NextAfterSequence = &next
			}
		} else {
			if result.HasNext && len(items) > 0 {
				next := items[len(items)-1].Sequence().Int64()
				result.NextBeforeSequence = &next
			}
			slices.Reverse(items)
		}
		result.Items = items
		return nil, nil
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetReadPreference(readpref.Primary()))
	if err != nil {
		return ports.ListTimelineResult{}, fmt.Errorf("read timeline snapshot: %w", err)
	}
	return result, nil
}
