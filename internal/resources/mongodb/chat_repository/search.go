package chatrepository

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/search"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (repository *Repository) populateSearchWords(ctx context.Context, document *chatDocument) error {
	cursor, err := repository.collection.Database().Collection("users").Find(ctx,
		bson.M{"_id": bson.M{"$in": []string{document.ParticipantOneID, document.ParticipantTwoID}}},
		options.Find().SetProjection(bson.M{"_id": 1, "email": 1}))
	if err != nil {
		return fmt.Errorf("find participants for search: %w", err)
	}
	defer cursor.Close(ctx)
	document.ParticipantOneWords, document.ParticipantTwoWords = []string{}, []string{}
	for cursor.Next(ctx) {
		var account struct {
			ID    string `bson:"_id"`
			Email string `bson:"email"`
		}
		if err := cursor.Decode(&account); err != nil {
			return err
		}
		if account.ID == document.ParticipantOneID {
			document.ParticipantOneWords = search.Words(account.Email)
		}
		if account.ID == document.ParticipantTwoID {
			document.ParticipantTwoWords = search.Words(account.Email)
		}
	}
	return cursor.Err()
}

func searchChatsFilter(params ports.ListChatsParams) bson.D {
	branches := bson.A{}
	for _, fields := range [][2]string{{"participant_one_id", "participant_two_words"}, {"participant_two_id", "participant_one_words"}} {
		branch := bson.D{{Key: fields[0], Value: params.ParticipantID.String()}, {Key: fields[1], Value: params.SearchWords[0]}}
		if len(params.SearchWords) > 1 {
			branch = append(branch, bson.E{Key: "$and", Value: bson.A{bson.M{fields[1]: bson.M{"$all": params.SearchWords}}}})
		}
		branches = append(branches, branch)
	}
	filter := bson.D{{Key: "$or", Value: branches}}
	if params.After != nil {
		filter = append(filter, bson.E{Key: "$and", Value: bson.A{bson.M{"$or": bson.A{
			bson.M{"created_at": bson.M{"$lt": params.After.SummarySortAt}},
			bson.M{"created_at": params.After.SummarySortAt, "_id": bson.M{"$lt": params.After.ChatID.String()}},
		}}}})
	}
	return filter
}

// BackfillSearchWords processes only missing projections, with bounded batches.
// Emails are immutable in the current account API. An email-change operation must
// update these participant projections as part of that future feature.
func (repository *Repository) BackfillSearchWords(ctx context.Context) error {
	filter := bson.M{"$or": bson.A{bson.M{"participant_one_words": bson.M{"$exists": false}}, bson.M{"participant_two_words": bson.M{"$exists": false}}}}
	cursor, err := repository.collection.Find(ctx, filter, options.Find().SetBatchSize(100).SetSort(bson.D{{Key: "_id", Value: 1}}).SetHint("_id_"))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	updates := make([]mongo.WriteModel, 0, 100)
	flush := func() error {
		if len(updates) == 0 {
			return nil
		}
		_, err := repository.collection.BulkWrite(ctx, updates, options.BulkWrite().SetOrdered(false))
		updates = updates[:0]
		return err
	}
	for cursor.Next(ctx) {
		var document chatDocument
		if err := cursor.Decode(&document); err != nil {
			return err
		}
		if err := repository.populateSearchWords(ctx, &document); err != nil {
			return err
		}
		updates = append(updates, mongo.NewUpdateOneModel().SetFilter(bson.M{"_id": document.ID}).SetUpdate(bson.M{"$set": bson.M{
			"participant_one_words": document.ParticipantOneWords, "participant_two_words": document.ParticipantTwoWords,
		}}))
		if len(updates) == 100 {
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
