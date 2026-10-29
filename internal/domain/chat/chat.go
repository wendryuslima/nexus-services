package chat

import (
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type ID struct {
	value string
}

func ParseID(rawID string) (ID, error) {
	normalizedID := strings.TrimSpace(rawID)
	if normalizedID == "" {
		return ID{}, ErrInvalidID
	}

	return ID{value: normalizedID}, nil
}

func (id ID) String() string {
	return id.value
}

type Chat struct {
	id               ID
	participantOneID user.ID
	participantTwoID user.ID
	createdAt        time.Time
	updatedAt        time.Time
	summarySortAt    time.Time
	lastSequence     int64
}

func NewDirect(id ID, firstParticipantID user.ID, secondParticipantID user.ID, now time.Time) (*Chat, error) {
	return RestoreDirect(id, firstParticipantID, secondParticipantID, now, now, now)
}

func RestoreDirect(id ID, firstParticipantID user.ID, secondParticipantID user.ID, createdAt time.Time, updatedAt time.Time, summarySortAt time.Time, persistedSequence ...int64) (*Chat, error) {
	var lastSequence int64
	if len(persistedSequence) > 1 {
		return nil, ErrInvalidSequence
	}
	if len(persistedSequence) == 1 {
		lastSequence = persistedSequence[0]
	}
	if lastSequence < 0 || lastSequence > 1<<53-1 {
		return nil, ErrInvalidSequence
	}
	if id.String() == "" {
		return nil, ErrInvalidID
	}

	if firstParticipantID.String() == "" || secondParticipantID.String() == "" {
		return nil, ErrInvalidParticipant
	}

	if firstParticipantID.String() == secondParticipantID.String() {
		return nil, ErrSameParticipant
	}

	if createdAt.IsZero() {
		return nil, ErrInvalidCreatedAt
	}

	if updatedAt.IsZero() || updatedAt.Before(createdAt) {
		return nil, ErrInvalidUpdatedAt
	}

	if summarySortAt.IsZero() || summarySortAt.Before(createdAt) {
		return nil, ErrInvalidSummarySortAt
	}

	participantOneID, participantTwoID := orderParticipants(firstParticipantID, secondParticipantID)
	return &Chat{
		id:               id,
		participantOneID: participantOneID,
		participantTwoID: participantTwoID,
		createdAt:        createdAt.UTC(),
		updatedAt:        updatedAt.UTC(),
		summarySortAt:    summarySortAt.UTC(),
		lastSequence:     lastSequence,
	}, nil
}

func (chat *Chat) ID() ID {
	return chat.id
}

func (chat *Chat) LastSequence() int64 { return chat.lastSequence }

func (chat *Chat) ParticipantOneID() user.ID {
	return chat.participantOneID
}

func (chat *Chat) ParticipantTwoID() user.ID {
	return chat.participantTwoID
}

func (chat *Chat) CreatedAt() time.Time {
	return chat.createdAt
}

func (chat *Chat) UpdatedAt() time.Time {
	return chat.updatedAt
}

func (chat *Chat) SummarySortAt() time.Time {
	return chat.summarySortAt
}

func (chat *Chat) HasParticipant(userID user.ID) bool {
	return chat.participantOneID.String() == userID.String() || chat.participantTwoID.String() == userID.String()
}

func (chat *Chat) RelatedUserID(currentUserID user.ID) (user.ID, error) {
	switch currentUserID.String() {
	case chat.participantOneID.String():
		return chat.participantTwoID, nil
	case chat.participantTwoID.String():
		return chat.participantOneID, nil
	default:
		return user.ID{}, ErrUserNotParticipant
	}
}

func orderParticipants(firstParticipantID user.ID, secondParticipantID user.ID) (user.ID, user.ID) {
	if firstParticipantID.String() < secondParticipantID.String() {
		return firstParticipantID, secondParticipantID
	}

	return secondParticipantID, firstParticipantID
}
