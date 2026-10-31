package messagestore

import (
	"context"
	"errors"
	"fmt"
	"github.com/wendryuslima/nexus-services/internal/domain/image"
	"strings"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type chatSequenceDocument struct {
	LastSequence int64 `bson:"last_sequence"`
}

func (repository *Repository) CreateWithOutbox(
	ctx context.Context,
	params ports.CreateMessageParams,
) (ports.CreateMessageResult, error) {
	if err := validateCreateParams(params); err != nil {
		return ports.CreateMessageResult{}, err
	}

	existingItem, found, err := repository.findExistingMessage(
		ctx,
		params,
	)
	if err != nil {
		return ports.CreateMessageResult{}, fmt.Errorf(
			"find existing idempotent message: %w",
			err,
		)
	}

	if found {
		return resolveExistingMessage(existingItem, params)
	}

	sequence, err := repository.createTransaction(ctx, params)
	if err != nil {

		if mongo.IsDuplicateKeyError(err) || errors.Is(err, ports.ErrImageUnavailable) {
			existingItem, found, lookupErr :=
				repository.findExistingMessage(ctx, params)
			if lookupErr != nil {
				return ports.CreateMessageResult{}, fmt.Errorf(
					"find message after duplicate key: %w",
					lookupErr,
				)
			}

			if found {
				return resolveExistingMessage(
					existingItem,
					params,
				)
			}
		}

		if errors.Is(err, ports.ErrChatNotFound) {
			return ports.CreateMessageResult{},
				ports.ErrChatNotFound
		}

		return ports.CreateMessageResult{}, fmt.Errorf(
			"persist message transaction: %w",
			err,
		)
	}

	document := newTimelineItemDocument(params, sequence)

	item, err := document.toDomain()
	if err != nil {
		return ports.CreateMessageResult{}, fmt.Errorf(
			"restore created message: %w",
			err,
		)
	}

	return ports.CreateMessageResult{
		Item:    item,
		Created: true,
	}, nil
}

func validateCreateParams(
	params ports.CreateMessageParams,
) error {
	if params.MessageID.String() == "" {
		return fmt.Errorf(
			"%w: message id cannot be empty",
			ErrInvalidCreateParams,
		)
	}

	if params.ChatID.String() == "" {
		return fmt.Errorf(
			"%w: chat id cannot be empty",
			ErrInvalidCreateParams,
		)
	}

	normalizedEventID := strings.TrimSpace(params.EventID)
	if normalizedEventID == "" ||
		normalizedEventID != params.EventID {
		return fmt.Errorf(
			"%w: event id is invalid",
			ErrInvalidCreateParams,
		)
	}

	if params.CreatedAt.IsZero() {
		return fmt.Errorf(
			"%w: created at cannot be zero",
			ErrInvalidCreateParams,
		)
	}

	message := params.Message
	if message.ClientMessageID().String() == "" {
		return fmt.Errorf(
			"%w: client message id cannot be empty",
			ErrInvalidCreateParams,
		)
	}

	if message.SenderID().String() == "" {
		return fmt.Errorf(
			"%w: sender id cannot be empty",
			ErrInvalidCreateParams,
		)
	}

	content := message.Content()
	if !content.Valid() {
		return fmt.Errorf(
			"%w: message content is invalid",
			ErrInvalidCreateParams,
		)
	}

	return nil
}

func (repository *Repository) findExistingMessage(
	ctx context.Context,
	params ports.CreateMessageParams,
) (*timeline.Item, bool, error) {
	filter := bson.D{
		{
			Key:   "message.sender_id",
			Value: params.Message.SenderID().String(),
		},
		{
			Key:   "message.client_message_id",
			Value: params.Message.ClientMessageID().String(),
		},
	}

	var document timelineItemDocument

	err := repository.timelineCollection.
		FindOne(ctx, filter).
		Decode(&document)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf(
			"find timeline item by idempotency key: %w",
			err,
		)
	}

	item, err := document.toDomain()
	if err != nil {
		return nil, false, fmt.Errorf(
			"convert existing timeline item to domain: %w",
			err,
		)
	}

	return item, true, nil
}

func resolveExistingMessage(
	existingItem *timeline.Item,
	params ports.CreateMessageParams,
) (ports.CreateMessageResult, error) {
	if existingItem == nil {
		return ports.CreateMessageResult{},
			errors.New("existing message cannot be nil")
	}

	if !sameLogicalMessage(existingItem, params) {
		return ports.CreateMessageResult{},
			ports.ErrMessageIdempotencyConflict
	}

	return ports.CreateMessageResult{
		Item:    existingItem,
		Created: false,
	}, nil
}

func sameLogicalMessage(
	existingItem *timeline.Item,
	params ports.CreateMessageParams,
) bool {
	if existingItem.ChatID().String() != params.ChatID.String() {
		return false
	}

	existingMessage := existingItem.Message()
	requestedMessage := params.Message

	if existingMessage.ClientMessageID().String() !=
		requestedMessage.ClientMessageID().String() {
		return false
	}

	if existingMessage.SenderID().String() !=
		requestedMessage.SenderID().String() {
		return false
	}

	existingContent := existingMessage.Content()
	requestedContent := requestedMessage.Content()

	return existingContent.Type() == requestedContent.Type() &&
		existingContent.Value() == requestedContent.Value() && existingContent.ImageID() == requestedContent.ImageID() && existingContent.Caption() == requestedContent.Caption()
}

func (repository *Repository) createTransaction(
	ctx context.Context,
	params ports.CreateMessageParams,
) (timeline.Sequence, error) {
	var allocatedSequence timeline.Sequence

	err := repository.transactions.RunInTransaction(
		ctx,
		func(transactionContext context.Context) error {
			if params.Message.Content().Type() == timeline.ContentTypeImage {
				if repository.imagesCollection == nil {
					return errors.New("images collection is required for image messages")
				}
				result, err := repository.imagesCollection.UpdateOne(transactionContext, bson.M{
					"_id": params.Message.Content().ImageID(), "uploaded_by": params.Message.SenderID().String(), "chat_id": params.ChatID.String(), "status": image.Ready, "expires_at": bson.M{"$gt": params.CreatedAt},
				}, bson.M{"$set": bson.M{"status": image.Attached, "message_id": params.MessageID.String()}})
				if err != nil {
					return err
				}
				if result.ModifiedCount != 1 {
					return ports.ErrImageUnavailable
				}
			}
			sequence, err := repository.allocateSequence(
				transactionContext,
				params,
			)
			if err != nil {
				return err
			}

			allocatedSequence = sequence

			timelineDocument := newTimelineItemDocument(
				params,
				sequence,
			)

			if _, err := repository.timelineCollection.InsertOne(
				transactionContext,
				timelineDocument,
			); err != nil {
				return fmt.Errorf(
					"insert timeline item: %w",
					err,
				)
			}

			outboxDocument := newOutboxEventDocument(
				params,
				sequence,
			)

			if _, err := repository.outboxCollection.InsertOne(
				transactionContext,
				outboxDocument,
			); err != nil {
				return fmt.Errorf(
					"insert outbox event: %w",
					err,
				)
			}

			return nil
		},
	)
	if err != nil {
		return 0, err
	}

	if allocatedSequence.Int64() <= 0 {
		return 0, errors.New(
			"transaction completed without allocating a sequence",
		)
	}

	return allocatedSequence, nil
}

func (repository *Repository) allocateSequence(
	ctx context.Context,
	params ports.CreateMessageParams,
) (timeline.Sequence, error) {
	filter := bson.D{
		{
			Key:   "_id",
			Value: params.ChatID.String(),
		},
		{
			Key:   "participant_ids",
			Value: params.Message.SenderID().String(),
		},
	}

	update := bson.D{
		{
			Key: "$inc",
			Value: bson.D{
				{Key: "last_sequence", Value: int64(1)},
			},
		},
		{

			Key: "$max",
			Value: bson.D{
				{
					Key:   "summary_sort_at",
					Value: params.CreatedAt.UTC(),
				},
			},
		},
	}

	findOptions := options.FindOneAndUpdate().
		SetReturnDocument(options.After).
		SetProjection(bson.D{
			{Key: "last_sequence", Value: 1},
		})

	var document chatSequenceDocument

	err := repository.chatCollection.
		FindOneAndUpdate(
			ctx,
			filter,
			update,
			findOptions,
		).
		Decode(&document)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, ports.ErrChatNotFound
	}

	if err != nil {
		return 0, fmt.Errorf(
			"allocate chat sequence: %w",
			err,
		)
	}

	sequence, err := timeline.ParseSequence(
		document.LastSequence,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"parse allocated chat sequence: %w",
			err,
		)
	}

	return sequence, nil
}
