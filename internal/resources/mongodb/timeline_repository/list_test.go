package timelinerepository

import (
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

func TestBuildListResultReturnsChronologicalItemsAndOlderCursor(t *testing.T) {
	base := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	newest := repositoryTestItem(t, "item-3", base.Add(3*time.Minute))
	middle := repositoryTestItem(t, "item-2", base.Add(2*time.Minute))
	oldestReturned := repositoryTestItem(t, "item-1", base.Add(time.Minute))
	extra := repositoryTestItem(t, "item-0", base)

	result := buildListResult([]*timeline.Item{newest, middle, oldestReturned, extra}, 3)
	if !result.HasNext || result.NextCursor == nil {
		t.Fatal("expected another page and a cursor")
	}
	if result.NextCursor.ItemID.String() != "item-1" {
		t.Fatalf("expected cursor item-1, got %s", result.NextCursor.ItemID.String())
	}
	wantOrder := []string{"item-1", "item-2", "item-3"}
	for index, want := range wantOrder {
		if got := result.Items[index].ID().String(); got != want {
			t.Fatalf("item %d: expected %s, got %s", index, want, got)
		}
	}
}

func repositoryTestItem(t *testing.T, itemIDValue string, createdAt time.Time) *timeline.Item {
	t.Helper()
	itemID, _ := timeline.ParseID(itemIDValue)
	chatID, _ := chat.ParseID("chat-1")
	senderID, _ := user.ParseID("user-1")
	content, _ := timeline.NewMessageContent(timeline.ContentTypeText, "message")
	clientMessageID, _ := timeline.ParseClientMessageID("00000000-0000-4000-8000-000000000001")
	sequence, _ := timeline.ParseSequence(1)
	message, _ := timeline.NewMessage(clientMessageID, senderID, content)
	item, err := timeline.RestoreMessage(itemID, chatID, sequence, createdAt, createdAt, message)
	if err != nil {
		t.Fatalf("restore item: %v", err)
	}
	return item
}
