package timeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	domaintimeline "github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type ListCursorInput struct {
	ID        string
	CreatedAt string
}

type ListInput struct {
	CurrentUserID   string
	ChatID          string
	PageSize        int
	Cursor          *ListCursorInput
	SequenceOrder   bool
	BeforeSequence  *int64
	AfterSequence   *int64
	ThroughSequence *int64
}

type ListedContent struct {
	Type  string
	Value string
}

type ListedMessage struct {
	ClientMessageID string
	SenderID        string
	IsMine          bool
	Content         ListedContent
}

type ListedItem struct {
	Sequence  int64
	ID        string
	Kind      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Message   ListedMessage
}

type ListCursorOutput struct {
	ID        string
	CreatedAt time.Time
}

type ListPaginationOutput struct {
	NextBeforeSequence *int64
	NextAfterSequence  *int64
	HasNext            bool
	NextCursor         *ListCursorOutput
	PageSize           int
}

type ListOutput struct {
	SnapshotThroughSequence *int64
	Items                   []ListedItem
	Pagination              ListPaginationOutput
}

type ListDependencies struct {
	ChatRepository     ports.ChatRepository
	TimelineRepository ports.TimelineRepository
}

type ListUseCase struct {
	chatRepository     ports.ChatRepository
	timelineRepository ports.TimelineRepository
}

func NewListUseCase(dependencies ListDependencies) (*ListUseCase, error) {
	if dependencies.ChatRepository == nil {
		return nil, fmt.Errorf("%w: chat repository", ErrNilDependency)
	}
	if dependencies.TimelineRepository == nil {
		return nil, fmt.Errorf("%w: timeline repository", ErrNilDependency)
	}
	return &ListUseCase{
		chatRepository:     dependencies.ChatRepository,
		timelineRepository: dependencies.TimelineRepository,
	}, nil
}

func (useCase *ListUseCase) Execute(ctx context.Context, input ListInput) (ListOutput, error) {
	currentUserID, err := user.ParseID(input.CurrentUserID)
	if err != nil {
		return ListOutput{}, fmt.Errorf("%w: %v", ErrInvalidCurrentUserID, err)
	}
	chatID, err := chat.ParseID(input.ChatID)
	if err != nil {
		return ListOutput{}, fmt.Errorf("%w: %v", ErrInvalidChatID, err)
	}
	pageSize, err := normalizePageSize(input.PageSize)
	if err != nil {
		return ListOutput{}, err
	}
	cursor, err := parseCursor(input.Cursor)
	if err != nil {
		return ListOutput{}, err
	}
	if err := validateSequenceInput(input); err != nil {
		return ListOutput{}, err
	}

	conversation, err := useCase.chatRepository.FindByID(ctx, chatID)
	if errors.Is(err, ports.ErrChatNotFound) {
		return ListOutput{}, ErrChatNotFound
	}
	if err != nil {
		return ListOutput{}, fmt.Errorf("find chat: %w", err)
	}
	if conversation == nil {
		return ListOutput{}, errors.New("chat repository returned a nil chat")
	}
	if !conversation.HasParticipant(currentUserID) {
		return ListOutput{}, ErrChatNotFound
	}

	result, err := useCase.timelineRepository.ListByChat(ctx, ports.ListTimelineParams{
		ChatID:          chatID,
		Limit:           pageSize,
		Before:          cursor,
		SequenceOrder:   input.SequenceOrder,
		BeforeSequence:  input.BeforeSequence,
		AfterSequence:   input.AfterSequence,
		ThroughSequence: input.ThroughSequence,
	})
	if err != nil {
		if errors.Is(err, ports.ErrInvalidTimelinePagination) {
			return ListOutput{}, ErrInvalidCursor
		}
		return ListOutput{}, fmt.Errorf("list timeline by chat: %w", err)
	}

	items := make([]ListedItem, 0, len(result.Items))
	for _, item := range result.Items {
		if item == nil {
			return ListOutput{}, errors.New("timeline repository returned a nil item")
		}
		if item.ChatID().String() != chatID.String() {
			return ListOutput{}, errors.New("timeline repository returned an item from another chat")
		}
		message := item.Message()
		if !conversation.HasParticipant(message.SenderID()) {
			return ListOutput{}, errors.New("timeline repository returned a message from a non-participant")
		}
		content := message.Content()
		items = append(items, ListedItem{
			Sequence:  item.Sequence().Int64(),
			ID:        item.ID().String(),
			Kind:      string(item.Kind()),
			CreatedAt: item.CreatedAt(),
			UpdatedAt: item.UpdatedAt(),
			Message: ListedMessage{
				ClientMessageID: message.ClientMessageID().String(),
				SenderID:        message.SenderID().String(),
				IsMine:          message.SenderID().String() == currentUserID.String(),
				Content:         ListedContent{Type: string(content.Type()), Value: content.Value()},
			},
		})
	}

	var nextCursor *ListCursorOutput
	if !input.SequenceOrder && input.AfterSequence == nil {
		nextCursor, err = mapNextCursor(result)
	}
	if err != nil {
		return ListOutput{}, err
	}
	return ListOutput{
		SnapshotThroughSequence: result.SnapshotThroughSequence,
		Items:                   items,
		Pagination: ListPaginationOutput{
			NextBeforeSequence: result.NextBeforeSequence, NextAfterSequence: result.NextAfterSequence,
			HasNext: result.HasNext, NextCursor: nextCursor, PageSize: pageSize,
		},
	}, nil
}

func validateSequenceInput(input ListInput) error {
	if (input.SequenceOrder || input.AfterSequence != nil || input.BeforeSequence != nil || input.ThroughSequence != nil) && input.Cursor != nil {
		return ErrInvalidCursor
	}
	if input.BeforeSequence != nil && (!input.SequenceOrder || input.AfterSequence != nil) {
		return ErrInvalidCursor
	}
	if input.AfterSequence != nil && input.SequenceOrder {
		return ErrInvalidCursor
	}
	if input.ThroughSequence != nil && input.AfterSequence == nil {
		return ErrInvalidCursor
	}
	for _, value := range []*int64{input.BeforeSequence, input.AfterSequence, input.ThroughSequence} {
		if value != nil && (*value < 0 || *value > domaintimeline.MaxSequence) {
			return ErrInvalidCursor
		}
	}
	if input.BeforeSequence != nil && *input.BeforeSequence == 0 {
		return ErrInvalidCursor
	}
	if input.AfterSequence != nil && input.ThroughSequence != nil && *input.AfterSequence > *input.ThroughSequence {
		return ErrInvalidCursor
	}
	return nil
}

func normalizePageSize(requested int) (int, error) {
	if requested == 0 {
		return defaultPageSize, nil
	}
	if requested < 1 || requested > maxPageSize {
		return 0, ErrInvalidPageSize
	}
	return requested, nil
}

func parseCursor(input *ListCursorInput) (*ports.TimelineCursor, error) {
	if input == nil {
		return nil, nil
	}
	itemID, err := domaintimeline.ParseID(input.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: id: %v", ErrInvalidCursor, err)
	}
	rawCreatedAt := strings.TrimSpace(input.CreatedAt)
	createdAt, err := time.Parse(time.RFC3339Nano, rawCreatedAt)
	if rawCreatedAt == "" || err != nil {
		return nil, fmt.Errorf("%w: createdAt must be RFC 3339", ErrInvalidCursor)
	}
	return &ports.TimelineCursor{ItemID: itemID, CreatedAt: createdAt.UTC()}, nil
}

func mapNextCursor(result ports.ListTimelineResult) (*ListCursorOutput, error) {
	if !result.HasNext {
		return nil, nil
	}
	if result.NextCursor == nil {
		return nil, errors.New("timeline repository returned hasNext without a next cursor")
	}
	return &ListCursorOutput{
		ID: result.NextCursor.ItemID.String(), CreatedAt: result.NextCursor.CreatedAt.UTC(),
	}, nil
}
