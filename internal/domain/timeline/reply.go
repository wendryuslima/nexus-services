package timeline

import "github.com/wendryuslima/nexus-services/internal/domain/user"

// ReplyContext is a single-level snapshot, independent of the loaded timeline page.
type ReplyContext struct {
	SenderID user.ID
	Sequence Sequence
	Content  MessageContent
}

func (message Message) WithReply(id ID, reply *ReplyContext) Message {
	message.replyID = id
	message.replyTo = reply
	return message
}

func (message Message) ReplyID() ID            { return message.replyID }
func (message Message) ReplyTo() *ReplyContext { return message.replyTo }
