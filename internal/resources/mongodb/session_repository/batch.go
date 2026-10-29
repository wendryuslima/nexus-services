package sessionrepository

import (
	"context"
	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (repository *Repository) FindByIDs(ctx context.Context, ids []session.ID) ([]*session.Session, error) {
	result := make([]*session.Session, 0, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		if id.String() == "" {
			return nil, session.ErrInvalidID
		}
		keys = append(keys, id.String())
	}
	cursor, err := repository.collection.Find(ctx, bson.M{"_id": bson.M{"$in": keys}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var document sessionDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		stored, err := document.toDomain()
		if err != nil {
			return nil, err
		}
		result = append(result, stored)
	}
	return result, cursor.Err()
}
