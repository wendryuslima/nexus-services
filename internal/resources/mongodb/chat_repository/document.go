package chatrepository

import (
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type chatDocument struct {
	ID string `bson:"_id"`

	ParticipantOneID string `bson:"participant_one_id"`
	ParticipantTwoID string `bson:"participant_two_id"`

	ParticipantIDs []string `bson:"participant_ids"`

	CreatedAt     time.Time `bson:"created_at"`
	UpdatedAt     time.Time `bson:"updated_at"`
	SummarySortAt time.Time `bson:"summary_sort_at"`
}

func newChatDocument(conversation *chat.Chat) (chatDocument, error) {
	if conversation == nil {
		return chatDocument{}, ErrNilChat
	}

	participantOneID := conversation.ParticipantOneID().String()
	participantTwoID := conversation.ParticipantTwoID().String()

	return chatDocument{
		ID: conversation.ID().String(),

		ParticipantOneID: participantOneID,
		ParticipantTwoID: participantTwoID,

		ParticipantIDs: []string{
			participantOneID,
			participantTwoID,
		},

		CreatedAt:     conversation.CreatedAt().UTC(),
		UpdatedAt:     conversation.UpdatedAt().UTC(),
		SummarySortAt: conversation.SummarySortAt().UTC(),
	}, nil
}

func (document chatDocument) toDomain() (*chat.Chat, error) {
	chatID, err := chat.ParseID(document.ID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: parse chat id: %v",
			ErrInvalidDocument,
			err,
		)
	}

	participantOneID, err := user.ParseID(document.ParticipantOneID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: parse first participant id: %v",
			ErrInvalidDocument,
			err,
		)
	}

	participantTwoID, err := user.ParseID(document.ParticipantTwoID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: parse second participant id: %v",
			ErrInvalidDocument,
			err,
		)
	}

	if participantOneID.String() >= participantTwoID.String() {
		return nil, fmt.Errorf(
			"%w: participants are not in canonical order",
			ErrInvalidDocument,
		)
	}

	if !document.hasConsistentParticipantIDs(
		participantOneID,
		participantTwoID,
	) {
		return nil, fmt.Errorf(
			"%w: inconsistent participant ids",
			ErrInvalidDocument,
		)
	}

	conversation, err := chat.RestoreDirect(
		chatID,
		participantOneID,
		participantTwoID,
		document.CreatedAt,
		document.UpdatedAt,
		document.SummarySortAt,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: restore chat: %v",
			ErrInvalidDocument,
			err,
		)
	}

	return conversation, nil
}

func (document chatDocument) hasConsistentParticipantIDs(
	participantOneID user.ID,
	participantTwoID user.ID,
) bool {
	if len(document.ParticipantIDs) != 2 {
		return false
	}

	return document.ParticipantIDs[0] == participantOneID.String() &&
		document.ParticipantIDs[1] == participantTwoID.String()
}
