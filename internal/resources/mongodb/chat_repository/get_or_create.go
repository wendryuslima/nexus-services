package chatrepository

import (
	"context"
	"errors"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (repository *Repository) GetOrCreateDirect(
	ctx context.Context,
	candidate *chat.Chat,
) (*chat.Chat, bool, error) {
	candidateDocument, err := newChatDocument(candidate)
	if err != nil {
		return nil, false, err
	}

	filter := directParticipantsFilter(
		candidateDocument.ParticipantOneID,
		candidateDocument.ParticipantTwoID,
	)

	update := bson.D{
		{
			Key:   "$setOnInsert",
			Value: candidateDocument,
		},
	}

	var storedDocument chatDocument

	err = repository.collection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().
			SetUpsert(true).
			SetReturnDocument(options.Before),
	).Decode(&storedDocument)

	switch {
	case err == nil:
		return convertStoredChat(storedDocument)

	case errors.Is(err, mongo.ErrNoDocuments):

		return candidate, true, nil

	case isParticipantsDuplicateError(err):

		storedChat, findErr := repository.findDirectByParticipants(
			ctx,
			candidateDocument.ParticipantOneID,
			candidateDocument.ParticipantTwoID,
		)
		if findErr != nil {
			return nil, false, fmt.Errorf(
				"recover direct chat after duplicate key: %w",
				findErr,
			)
		}

		return storedChat, false, nil

	default:
		return nil, false, fmt.Errorf(
			"find or create direct chat document: %w",
			err,
		)
	}
}

func directParticipantsFilter(
	participantOneID string,
	participantTwoID string,
) bson.D {
	return bson.D{
		{Key: "participant_one_id", Value: participantOneID},
		{Key: "participant_two_id", Value: participantTwoID},
	}
}

func (repository *Repository) findDirectByParticipants(
	ctx context.Context,
	participantOneID string,
	participantTwoID string,
) (*chat.Chat, error) {
	var document chatDocument

	err := repository.collection.FindOne(
		ctx,
		directParticipantsFilter(
			participantOneID,
			participantTwoID,
		),
	).Decode(&document)
	if err != nil {
		return nil, fmt.Errorf(
			"find direct chat document by participants: %w",
			err,
		)
	}

	conversation, err := document.toDomain()
	if err != nil {
		return nil, fmt.Errorf(
			"convert direct chat document to domain: %w",
			err,
		)
	}

	return conversation, nil
}

func convertStoredChat(
	document chatDocument,
) (*chat.Chat, bool, error) {
	conversation, err := document.toDomain()
	if err != nil {
		return nil, false, fmt.Errorf(
			"convert direct chat document to domain: %w",
			err,
		)
	}

	return conversation, false, nil
}

func isParticipantsDuplicateError(err error) bool {
	if !mongo.IsDuplicateKeyError(err) {
		return false
	}

	var serverError mongo.ServerError
	if !errors.As(err, &serverError) {
		return false
	}

	return serverError.HasErrorMessage(
		chatParticipantsUniqueIndexName,
	)
}
