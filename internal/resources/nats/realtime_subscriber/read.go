package realtimesubscriber

import (
	"context"
	"encoding/json"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

func (s *Subscriber) PublishChatRead(ctx context.Context, participant user.ID, state ports.ChatReadState) error {
	if !ports.ValidRealtimeUserID(participant.String()) || !ports.ValidChatReadState(state) {
		return ports.ErrInvalidMessageCreatedEvent
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	event, err := json.Marshal(ports.RealtimeEvent{Version: ports.RealtimeProtocolVersion, Type: "chat.read", Data: data})
	if err != nil {
		return err
	}
	if err := s.conn.Publish("realtime.user."+participant.String(), event); err != nil {
		return err
	}
	flush, cancel := context.WithTimeout(ctx, s.flushTimeout)
	defer cancel()
	return s.conn.FlushWithContext(flush)
}
