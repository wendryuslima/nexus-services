package ports

import (
	"errors"
	"strings"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
)

func ValidMessageReply(id string, reply *MessageReply, sequence int64) bool {
	if id == "" {
		return reply == nil
	}
	return strings.TrimSpace(id) == id && len(id) <= 256 && reply != nil &&
		ValidRealtimeUserID(reply.SenderID) && reply.Sequence > 0 && reply.Sequence < sequence && ValidMessageContent(reply.Content)
}

var ErrReplyUnavailable = errors.New("reply message unavailable")

type MessageReply struct {
	SenderID string        `json:"senderId"`
	Sequence int64         `json:"sequence"`
	Content  OutboxContent `json:"content"`
}

func ReplyFromDomain(reply *timeline.ReplyContext) *MessageReply {
	if reply == nil {
		return nil
	}
	return &MessageReply{
		SenderID: reply.SenderID.String(), Sequence: reply.Sequence.Int64(),
		Content: OutboxContent{Type: string(reply.Content.Type()), Value: reply.Content.Value(), ImageID: reply.Content.ImageID(), Caption: reply.Content.Caption()},
	}
}
