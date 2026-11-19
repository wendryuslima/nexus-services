package ports

import (
	"encoding/json"
)

// RealtimeEvent is the transport envelope for validated public event types.
// Message creation retains its typed payload for the delivery pipeline.
type RealtimeEvent struct {
	Version    int             `json:"version"`
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
	EventID    string          `json:"eventId,omitempty"`
	OccurredAt string          `json:"occurredAt,omitempty"`
}

func ValidChatReadState(s ChatReadState) bool {
	return s.ChatID != "" && s.LastReadSequence >= 0 && s.LastSequence >= s.LastReadSequence && s.LastSequence <= 1<<53-1 && s.UnreadCount >= 0 && s.UnreadCount <= s.LastSequence-s.LastReadSequence
}

func ValidateRealtimeEvent(e RealtimeEvent) error {
	if e.Version != RealtimeProtocolVersion {
		return ErrInvalidMessageCreatedEvent
	}
	switch e.Type {
	case RealtimeMessageCreatedType:
		raw, _ := json.Marshal(e)
		var message RealtimeMessageCreated
		if json.Unmarshal(raw, &message) != nil {
			return ErrInvalidMessageCreatedEvent
		}
		return ValidateRealtimeMessage(message)
	case "chat.read":
		var state ChatReadState
		if json.Unmarshal(e.Data, &state) != nil || !ValidChatReadState(state) {
			return ErrInvalidMessageCreatedEvent
		}
		return nil
	default:
		return ErrInvalidMessageCreatedEvent
	}
}
