package chats

import (
	"context"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type MarkReadInput struct {
	ChatID, CurrentUserID string
	ThroughSequence       int64
}
type MarkReadUseCase struct {
	reads     ports.ChatReadRepository
	publisher ports.ChatReadPublisher
}

func NewMarkReadUseCase(reads ports.ChatReadRepository, publisher ports.ChatReadPublisher) (*MarkReadUseCase, error) {
	if reads == nil || publisher == nil {
		return nil, ErrNilDependency
	}
	return &MarkReadUseCase{reads: reads, publisher: publisher}, nil
}

func (u *MarkReadUseCase) Execute(ctx context.Context, input MarkReadInput) (ports.ChatReadState, error) {
	participant, err := user.ParseID(input.CurrentUserID)
	if err != nil {
		return ports.ChatReadState{}, ErrInvalidCurrentUserID
	}
	id, err := chat.ParseID(input.ChatID)
	if err != nil {
		return ports.ChatReadState{}, ports.ErrChatNotFound
	}
	if input.ThroughSequence < 0 || input.ThroughSequence > timeline.MaxSequence {
		return ports.ChatReadState{}, ports.ErrInvalidReadSequence
	}
	state, err := u.reads.MarkRead(ctx, id, participant, input.ThroughSequence)
	if err != nil {
		return ports.ChatReadState{}, err
	}
	// Repeated requests republish the watermark, allowing recovery after a failed
	// notification without changing already persisted read progress.
	if err := u.publisher.PublishChatRead(ctx, participant, state); err != nil {
		return ports.ChatReadState{}, fmt.Errorf("publish chat read: %w", err)
	}
	return state, nil
}
