package messagestore

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/ports"
	outboxrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/outbox_repository"
	timelinerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/timeline_repository"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestOutboxBSONIncludesClientMessageID(t *testing.T) {
	params := integrationCreateMessageParams(t, "message-1", "event-1", "Olá")
	raw, err := bson.Marshal(newOutboxEventDocument(params, timeline.Sequence(1)))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := bson.Raw(raw).Lookup("payload", "clientMessageId").StringValueOK()
	if !ok || value != params.Message.ClientMessageID().String() {
		t.Fatalf("client key missing in BSON: %q", value)
	}
}

func TestRealtimeSequenceRecoveryAndOutboxClaim(t *testing.T) {
	uri := os.Getenv(mongoDBTestURIEnvironment)
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI for transaction integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store, collections, cleanup := newMessageStoreIntegrationFixture(t, ctx, uri)
	defer cleanup()
	params := integrationCreateMessageParams(t, "message-1", "event-1", "Olá")
	insertIntegrationChat(t, ctx, collections.chats, params)
	for i := 1; i <= 112; i++ {
		current := params
		current.MessageID, _ = timeline.ParseID(fmt.Sprintf("message-%d", i))
		current.EventID = fmt.Sprintf("event-%d", i)
		clientID, _ := timeline.ParseClientMessageID(fmt.Sprintf("00000000-0000-4000-8000-%012d", i))
		current.Message, _ = timeline.NewMessage(clientID, params.Message.SenderID(), params.Message.Content())
		current.CreatedAt = params.CreatedAt.Add(-time.Duration(i) * time.Second)
		if _, err := store.CreateWithOutbox(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	repository, err := timelinerepository.NewRepository(collections.timeline)
	if err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListByChat(ctx, ports.ListTimelineParams{ChatID: params.ChatID, Limit: 50, SequenceOrder: true})
	if err != nil {
		t.Fatal(err)
	}
	if page.SnapshotThroughSequence == nil || *page.SnapshotThroughSequence != 112 || len(page.Items) != 50 || page.Items[0].Sequence().Int64() != 63 || page.Items[49].Sequence().Int64() != 112 {
		t.Fatalf("invalid initial snapshot: %+v", page)
	}
	if page.NextBeforeSequence == nil || *page.NextBeforeSequence != 63 {
		t.Fatal("wrong history cursor")
	}
	after := int64(0)
	var through *int64
	total := 0
	for {
		page, err = repository.ListByChat(ctx, ports.ListTimelineParams{ChatID: params.ChatID, Limit: 30, AfterSequence: &after, ThroughSequence: through})
		if err != nil {
			t.Fatal(err)
		}
		if through == nil {
			through = page.SnapshotThroughSequence
			extra := params
			extra.MessageID, _ = timeline.ParseID("message-113")
			extra.EventID = "event-113"
			if _, err := store.CreateWithOutbox(ctx, extra); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range page.Items {
			if item.Sequence().Int64() != after+1 {
				t.Fatal("recovery skipped a sequence")
			}
			after = item.Sequence().Int64()
			total++
		}
		if !page.HasNext {
			break
		}
		if page.NextAfterSequence == nil || *page.NextAfterSequence != after {
			t.Fatal("wrong incremental cursor")
		}
	}
	if total != 112 || after != 112 {
		t.Fatalf("incomplete recovery: %d", total)
	}
	outbox, err := outboxrepository.NewRepository(collections.outbox)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := outbox.ClaimNext(ctx, "test-worker", "lease-1", time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.Payload.ClientMessageID == "" {
		t.Fatal("claim lost client key")
	}
	if err := repository.CheckRealtimeData(ctx); err != nil {
		t.Fatalf("audit rejected consistent data: %v", err)
	}
	if _, err := collections.timeline.InsertOne(ctx, bson.M{"_id": "legacy", "chat_id": params.ChatID.String()}); err != nil {
		t.Fatal(err)
	}
	if err := repository.CheckRealtimeData(ctx); err == nil {
		t.Fatal("audit silently accepted legacy data")
	}
}
