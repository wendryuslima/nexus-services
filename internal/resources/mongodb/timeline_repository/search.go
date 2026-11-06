package timelinerepository

import (
	"context"

	"github.com/wendryuslima/nexus-services/internal/domain/search"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// BackfillSearchWords is resumable and never loads the full history into memory.
func (repository *Repository) BackfillSearchWords(ctx context.Context) error {
	cursor, err := repository.collection.Find(ctx, bson.M{"search_words": bson.M{"$exists": false}},
		options.Find().SetProjection(bson.M{"_id": 1, "message.content": 1}).SetBatchSize(200).
			SetSort(bson.D{{Key: "_id", Value: 1}}).SetHint("_id_"))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	updates := make([]mongo.WriteModel, 0, 200)
	flush := func() error {
		if len(updates) == 0 {
			return nil
		}
		_, err := repository.collection.BulkWrite(ctx, updates, options.BulkWrite().SetOrdered(false))
		updates = updates[:0]
		return err
	}
	for cursor.Next(ctx) {
		var document timelineItemDocument
		if err := cursor.Decode(&document); err != nil {
			return err
		}
		words := search.Words(document.Message.Content.Value + " " + document.Message.Content.Caption)
		updates = append(updates, mongo.NewUpdateOneModel().SetFilter(bson.M{"_id": document.ID, "search_words": bson.M{"$exists": false}}).SetUpdate(bson.M{"$set": bson.M{"search_words": words}}))
		if len(updates) == 200 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	return flush()
}
