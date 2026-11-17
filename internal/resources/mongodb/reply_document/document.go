package replydocument

import (
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type Content struct {
	Type    string `bson:"type" json:"type"`
	Value   string `bson:"value,omitempty" json:"value,omitempty"`
	ImageID string `bson:"imageId,omitempty" json:"imageId,omitempty"`
	Caption string `bson:"caption,omitempty" json:"caption,omitempty"`
}

type Document struct {
	SenderID string  `bson:"senderId" json:"senderId"`
	Sequence int64   `bson:"sequence" json:"sequence"`
	Content  Content `bson:"content" json:"content"`
}

func FromDomain(reply *timeline.ReplyContext) *Document {
	if reply == nil {
		return nil
	}
	return &Document{SenderID: reply.SenderID.String(), Sequence: reply.Sequence.Int64(), Content: Content{
		Type: string(reply.Content.Type()), Value: reply.Content.Value(), ImageID: reply.Content.ImageID(), Caption: reply.Content.Caption(),
	}}
}

func (document *Document) ToDomain() (*timeline.ReplyContext, error) {
	if document == nil {
		return nil, nil
	}
	sender, err := user.ParseID(document.SenderID)
	if err != nil {
		return nil, err
	}
	sequence, err := timeline.ParseSequence(document.Sequence)
	if err != nil {
		return nil, err
	}
	content, err := timeline.RestoreContent(timeline.ContentType(document.Content.Type), document.Content.Value, document.Content.ImageID, document.Content.Caption)
	if err != nil {
		return nil, err
	}
	return &timeline.ReplyContext{SenderID: sender, Sequence: sequence, Content: content}, nil
}

func (document *Document) ToPort() *ports.MessageReply {
	if document == nil {
		return nil
	}
	return &ports.MessageReply{SenderID: document.SenderID, Sequence: document.Sequence, Content: ports.OutboxContent{
		Type: document.Content.Type, Value: document.Content.Value, ImageID: document.Content.ImageID, Caption: document.Content.Caption,
	}}
}
