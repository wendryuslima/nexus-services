package ports

import (
	"context"
	"errors"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

var ErrInvalidReadSequence = errors.New("invalid read sequence")

// ChatReadState is a participant-specific snapshot, bounded by LastSequence.
type ChatReadState struct {
	ChatID           string `json:"chatId" bson:"chatId"`
	UnreadCount      int64  `json:"unreadCount" bson:"unreadCount"`
	LastReadSequence int64  `json:"lastReadSequence" bson:"lastReadSequence"`
	LastSequence     int64  `json:"lastSequence" bson:"lastSequence"`
}

type ChatReadRepository interface {
	ReadStates(context.Context, []chat.ID, user.ID) (map[string]ChatReadState, error)
	MarkRead(context.Context, chat.ID, user.ID, int64) (ChatReadState, error)
}

type ChatReadPublisher interface {
	PublishChatRead(context.Context, user.ID, ChatReadState) error
}
