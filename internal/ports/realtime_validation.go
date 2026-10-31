package ports

import (
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"strings"
)

func ValidRealtimeUserID(id string) bool {
	return id != "" && len(id) <= 256 && strings.IndexFunc(id, func(r rune) bool { return r <= 32 || r == 127 || strings.ContainsRune(".*>", r) }) < 0
}

func ValidateRealtimeMessage(event RealtimeMessageCreated) error {
	data := event.Data
	if event.Version != RealtimeProtocolVersion || event.Type != RealtimeMessageCreatedType || strings.TrimSpace(event.EventID) == "" || event.OccurredAt.IsZero() || strings.TrimSpace(data.ID) == "" || strings.TrimSpace(data.ChatID) == "" || !ValidRealtimeUserID(data.SenderID) || data.Sequence <= 0 || data.Sequence > timeline.MaxSequence || data.CreatedAt.IsZero() || !ValidMessageContent(data.Content) {
		return ErrInvalidMessageCreatedEvent
	}
	if data.ClientMessageID != "" {
		if _, err := timeline.ParseClientMessageID(data.ClientMessageID); err != nil {
			return ErrInvalidMessageCreatedEvent
		}
	}
	return nil
}
