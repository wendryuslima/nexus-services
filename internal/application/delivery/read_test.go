package delivery

import (
	"context"
	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"testing"
)

type readStub struct{}

func (*readStub) ReadStates(_ context.Context, ids []chat.ID, participant user.ID) (map[string]ports.ChatReadState, error) {
	states := map[string]ports.ChatReadState{}
	for _, id := range ids {
		states[id.String()] = ports.ChatReadState{ChatID: id.String(), LastSequence: 100, UnreadCount: 1}
	}
	return states, nil
}
func (*readStub) MarkRead(context.Context, chat.ID, user.ID, int64) (ports.ChatReadState, error) {
	panic("unexpected MarkRead")
}

func TestDispatchRequiresReadRepository(t *testing.T) {
	if _, err := NewDispatchMessageCreated(&chatLookupStub{}, &realtimeStub{}, nil); err == nil {
		t.Fatal("expected missing dependency error")
	}
}
