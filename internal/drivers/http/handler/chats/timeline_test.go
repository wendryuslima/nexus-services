package chats

import (
	"net/url"
	"testing"
)

func TestTimelineRejectsIncompatibleAndUnsafePagination(t *testing.T) {
	for _, raw := range []string{"pageSize=0", "pageSize=101", "afterSequence=-1", "afterSequence=1.5", "afterSequence=9007199254740992", "afterSequence=1&afterSequence=2", "beforeSequence=3", "throughSequence=5", "afterSequence=5&throughSequence=4", "order=sequence&afterSequence=1", "order=sequence&beforeSequence=0", "order=sequence&cursorId=x&cursorCreatedAt=2026-01-01T00:00:00Z", "order=unknown"} {
		query, _ := url.ParseQuery(raw)
		if _, err := parseTimelineInput(query, "user-1", "chat-1"); err == nil {
			t.Errorf("accepted invalid pagination: %s", raw)
		}
	}
	for _, raw := range []string{"order=sequence&pageSize=50", "order=sequence&beforeSequence=151", "afterSequence=0", "afterSequence=182&throughSequence=200"} {
		query, _ := url.ParseQuery(raw)
		if _, err := parseTimelineInput(query, "user-1", "chat-1"); err != nil {
			t.Errorf("rejected valid pagination: %s: %v", raw, err)
		}
	}
}
