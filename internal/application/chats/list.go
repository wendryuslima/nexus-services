package chats

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	defaultChatPageSize = 30
	maxChatPageSize     = 100
)

type ListCursorInput struct {
	ID     string
	SortAt string
}

type ListInput struct {
	CurrentUserID string
	Page          int
	PageSize      int
	Cursor        *ListCursorInput
}

type ListedChat struct {
	ID            string
	RelatedUser   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	SummarySortAt time.Time
}

type ListCursorOutput struct {
	ID     string
	SortAt time.Time
}

type ListPaginationOutput struct {
	HasNext    bool
	NextCursor *ListCursorOutput
	Page       int
	PageSize   int
	TotalItems int64
	TotalPages int64
}

type ListOutput struct {
	Chats      []ListedChat
	Pagination ListPaginationOutput
}

type ListUseCase struct {
	chatRepository ports.ChatRepository
}

func NewListUseCase(chatRepository ports.ChatRepository) (*ListUseCase, error) {
	if chatRepository == nil {
		return nil, fmt.Errorf("%w: chat repository",
			ErrNilDependency)
	}

	return &ListUseCase{
		chatRepository: chatRepository,
	}, nil
}

func (useCase *ListUseCase) Execute(ctx context.Context, input ListInput) (ListOutput, error) {
	currentUserID, err := user.ParseID(input.CurrentUserID)
	if err != nil {
		return ListOutput{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidCurrentUserID,
			err,
		)
	}
	page, pageSize, err := normalizeListPagination(input.Page, input.PageSize)
	if err != nil {
		return ListOutput{}, err
	}

	cursor, err := parseListCursor(input.Cursor)
	if err != nil {
		return ListOutput{}, err
	}

	if err := validateCursorForPage(page, cursor); err != nil {
		return ListOutput{}, err
	}
	result, err := useCase.chatRepository.ListByParticipant(ctx, ports.ListChatsParams{
		ParticipantID: currentUserID,
		Limit:         pageSize,
		After:         cursor,
	})
	if err != nil {
		return ListOutput{}, fmt.Errorf("list chats by participant: %w",
			err)
	}
	listedChats := make([]ListedChat, 0, len(result.Chats))

	for _, conversation := range result.Chats {
		if conversation == nil {
			return ListOutput{}, fmt.Errorf("chat repository returned a nil chat")
		}
		relatedUserID, err := conversation.RelatedUserID(currentUserID)
		if err != nil {
			return ListOutput{}, fmt.Errorf("resolve related user from listed chat: %w",
				err)
		}

		listedChats = append(listedChats, ListedChat{
			ID:            conversation.ID().String(),
			RelatedUser:   relatedUserID.String(),
			CreatedAt:     conversation.CreatedAt(),
			UpdatedAt:     conversation.UpdatedAt(),
			SummarySortAt: conversation.SummarySortAt(),
		})
	}
	nextCursor, err := mapNextCursor(result)
	if err != nil {
		return ListOutput{}, err
	}
	return ListOutput{
		Chats: listedChats,
		Pagination: ListPaginationOutput{
			HasNext:    result.HasNext,
			NextCursor: nextCursor,
			Page:       page,
			PageSize:   pageSize,
			TotalItems: result.TotalItems,
			TotalPages: calculateTotalPages(
				result.TotalItems,
				pageSize,
			),
		},
	}, nil
}

func normalizeListPagination(requestedPage int, requestedPageSize int) (int, int, error) {
	page := requestedPage
	if page == 0 {
		page = 1
	}

	if page < 1 {
		return 0, 0, ErrInvalidPage
	}
	pageSize := requestedPageSize

	if pageSize == 0 {
		pageSize = defaultChatPageSize
	}

	if pageSize < 1 || pageSize > maxChatPageSize {
		return 0, 0, ErrInvalidPageSize
	}

	return page, pageSize, nil
}

func parseListCursor(input *ListCursorInput) (*ports.ChatListCursor, error) {
	if input == nil {
		return nil, nil
	}

	chatID, err := chat.ParseID(input.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: id: %v",
			ErrInvalidCursor,
			err)
	}
	rawSortAt := strings.TrimSpace(input.SortAt)
	if rawSortAt == "" {
		return nil, fmt.Errorf("%w: sortAt is required",
			ErrInvalidCursor)
	}

	sortAt, err := time.Parse(time.RFC3339Nano, rawSortAt)
	if err != nil {
		return nil, fmt.Errorf("%w: sortAt must be RFC 3339: %v",
			ErrInvalidCursor,
			err)
	}

	return &ports.ChatListCursor{
		ChatID:        chatID,
		SummarySortAt: sortAt.UTC(),
	}, nil
}

func validateCursorForPage(page int, cursor *ports.ChatListCursor) error {
	if page == 1 && cursor != nil {
		return ErrInconsistentPagination
	}

	if page > 1 && cursor == nil {
		return ErrInconsistentPagination
	}

	return nil
}

func mapNextCursor(result ports.ListChatsResult) (*ListCursorOutput, error) {
	if !result.HasNext {
		return nil, nil
	}

	if result.NextCursor == nil {
		return nil, fmt.Errorf("chat repository returned hasNext without a next cursor")
	}

	return &ListCursorOutput{
		ID:     result.NextCursor.ChatID.String(),
		SortAt: result.NextCursor.SummarySortAt.UTC(),
	}, nil
}

func calculateTotalPages(totalItems int64, pageSize int) int64 {
	if totalItems == 0 {
		return 0
	}

	return (totalItems + int64(pageSize) - 1) / int64(pageSize)
}
