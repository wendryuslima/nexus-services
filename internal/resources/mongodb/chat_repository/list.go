package chatrepository

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (repository *Repository) ListByParticipant(ctx context.Context, params ports.ListChatsParams) (ports.ListChatsResult, error) {
	if err := validateListChatParams(params); err != nil {
		return ports.ListChatsResult{}, err
	}

	baseFilter := bson.D{
		{
			Key:   "participant_ids",
			Value: params.ParticipantID.String(),
		},
	}
	var totalItems int64
	var err error
	if len(params.SearchWords) == 0 {
		totalItems, err = repository.collection.CountDocuments(ctx, baseFilter)
	}
	if err != nil {
		return ports.ListChatsResult{}, fmt.Errorf("count chat documents by participant: %w ", err)

	}
	filter := buildListChatsFilter(params)
	sortField := "summary_sort_at"
	if len(params.SearchWords) > 0 {
		filter = searchChatsFilter(params)
		sortField = "created_at"
	}
	findOptions := options.Find().SetSort(bson.D{
		{Key: sortField, Value: -1},
		{Key: "_id", Value: -1},
	}).
		SetLimit(int64(params.Limit) + 1)
	mongoCursor, err := repository.collection.Find(ctx, filter, findOptions)
	if err != nil {
		return ports.ListChatsResult{}, fmt.Errorf("find chat documents by participant: %w", err)
	}
	defer mongoCursor.Close(ctx)

	conversations := make([]*chat.Chat, 0, params.Limit+1)
	for mongoCursor.Next(ctx) {
		var document chatDocument

		if err := mongoCursor.Decode(&document); err != nil {
			return ports.ListChatsResult{}, fmt.Errorf("decode chat document: %w", err)
		}

		conversation, err := document.toDomain()
		if err != nil {
			return ports.ListChatsResult{}, fmt.Errorf("convert chat document to domain: %w",
				err)
		}
		conversations = append(conversations, conversation)
	}

	if err := mongoCursor.Err(); err != nil {
		return ports.ListChatsResult{}, fmt.Errorf("iterate over chat documents: %w",
			err)
	}

	result := buildListChatResult(conversations, params.Limit, totalItems)
	if len(params.SearchWords) > 0 && result.NextCursor != nil {
		result.NextCursor.SummarySortAt = result.Chats[len(result.Chats)-1].CreatedAt()
	}
	return result, nil
}

func validateListChatParams(params ports.ListChatsParams) error {
	if params.ParticipantID.String() == "" {
		return ErrInvalidListParticipant
	}

	if params.Limit <= 0 {
		return ErrInvalidListLimit
	}

	if params.After == nil {
		return nil
	}

	if params.After.ChatID.String() == "" || params.After.SummarySortAt.IsZero() {
		return ErrInvalidListCursor
	}

	return nil
}

func buildListChatsFilter(params ports.ListChatsParams) bson.D {
	filter := bson.D{
		{
			Key:   "participant_ids",
			Value: params.ParticipantID.String(),
		},
	}

	if params.After == nil {
		return filter
	}
	cursorSortAt := params.After.SummarySortAt.UTC()
	cursorChatID := params.After.ChatID.String()

	cursorFilter := bson.E{
		Key: "$or",
		Value: bson.A{
			bson.D{
				{
					Key: "summary_sort_at",
					Value: bson.D{
						{Key: "$lt", Value: cursorSortAt},
					},
				},
			},
			bson.D{
				{
					Key:   "summary_sort_at",
					Value: cursorSortAt,
				},
				{
					Key: "_id",
					Value: bson.D{
						{Key: "$lt", Value: cursorChatID},
					},
				},
			},
		},
	}
	return append(filter, cursorFilter)
}

func buildListChatResult(conversations []*chat.Chat, limit int, totalItems int64) ports.ListChatsResult {
	hasNext := len(conversations) > limit
	if hasNext {
		conversations = conversations[:limit]
	}

	var nextCursor *ports.ChatListCursor

	if hasNext {
		lastConversation := conversations[len(conversations)-1]

		nextCursor = &ports.ChatListCursor{
			ChatID:        lastConversation.ID(),
			SummarySortAt: lastConversation.SummarySortAt(),
		}
	}

	return ports.ListChatsResult{
		Chats:      conversations,
		HasNext:    hasNext,
		NextCursor: nextCursor,
		TotalItems: totalItems,
	}
}
