package ports

import (
	"context"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type ChatListCursor struct {
	ChatID        chat.ID
	SummarySortAt time.Time
}

type ListChatsParams struct {
	ParticipantID user.ID
	Limit         int
	After         *ChatListCursor
}

type ListChatsResult struct {
	Chats      []*chat.Chat
	HasNext    bool
	NextCursor *ChatListCursor
	TotalItems int64
}

type ChatRepository interface {
	GetOrCreateDirect(ctx context.Context, candidate *chat.Chat) (storedChat *chat.Chat, created bool, err error)
	ListByParticipant(ctx context.Context, params ListChatsParams) (ListChatsResult, error)
}
