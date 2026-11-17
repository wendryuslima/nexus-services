package messagestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	outboxrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/outbox_repository"
	timelinerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/timeline_repository"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestRepliesPersistAcrossPagesAndOutbox(t *testing.T) {
	uri := os.Getenv(mongoDBTestURIEnvironment)
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store, collections, cleanup := newMessageStoreIntegrationFixture(t, ctx, uri)
	defer cleanup()
	original := integrationCreateMessageParams(t, "original", "original-event", "Mensagem original")
	insertIntegrationChat(t, ctx, collections.chats, original)
	if _, err := store.CreateWithOutbox(ctx, original); err != nil {
		t.Fatal(err)
	}

	for index, sender := range []string{"user-1", "user-2"} {
		params := integrationCreateMessageParams(t, fmt.Sprintf("reply-%d", index), fmt.Sprintf("reply-event-%d", index), "Resposta")
		clientID, _ := timeline.ParseClientMessageID(fmt.Sprintf("00000000-0000-4000-8000-%012d", index+1))
		senderID, _ := user.ParseID(sender)
		params.Message, _ = timeline.NewMessage(clientID, senderID, params.Message.Content())
		params.Message = params.Message.WithReply(original.MessageID, nil)
		result, err := store.CreateWithOutbox(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		assertReplySnapshot(t, result.Item, original)
		retry, err := store.CreateWithOutbox(ctx, params)
		if err != nil || retry.Created {
			t.Fatalf("retry: %+v %v", retry, err)
		}
		assertReplySnapshot(t, retry.Item, original)
		changed := params
		changed.Message = changed.Message.WithReply(timeline.ID{}, nil)
		if _, err := store.CreateWithOutbox(ctx, changed); !errors.Is(err, ports.ErrMessageIdempotencyConflict) {
			t.Fatalf("reply identity was ignored: %v", err)
		}
	}

	repository, err := timelinerepository.NewRepository(collections.timeline)
	if err != nil {
		t.Fatal(err)
	}
	around := int64(3)
	after := int64(2)
	for _, params := range []ports.ListTimelineParams{
		{ChatID: original.ChatID, Limit: 1},
		{ChatID: original.ChatID, Limit: 1, SequenceOrder: true},
		{ChatID: original.ChatID, Limit: 1, AfterSequence: &after},
		{ChatID: original.ChatID, Limit: 1, SequenceOrder: true, AroundSequence: &around},
	} {
		page, err := repository.ListByChat(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0].ID() == original.MessageID {
			t.Fatalf("unexpected page: %+v", page)
		}
		assertReplySnapshot(t, page.Items[0], original)
	}

	outbox, err := outboxrepository.NewRepository(collections.outbox)
	if err != nil {
		t.Fatal(err)
	}
	replies := 0
	for i := 0; i < 3; i++ {
		event, err := outbox.ClaimNext(ctx, "reply-test", fmt.Sprint(i), time.Now().UTC(), time.Minute)
		if err != nil || event == nil {
			t.Fatalf("claim: %+v %v", event, err)
		}
		if event.Payload.ReplyID != "" {
			replies++
			if event.Payload.ReplyID != "original" || event.Payload.ReplyTo == nil || event.Payload.ReplyTo.Content.Value != "Mensagem original" || event.Payload.ReplyTo.Sequence != 1 {
				t.Fatalf("outbox lost reply: %+v", event.Payload)
			}
		}
	}
	if replies != 2 {
		t.Fatalf("expected two reply events, got %d", replies)
	}

	foreign := newTimelineItemDocument(original, timeline.Sequence(1))
	foreign.ID, foreign.ChatID = "foreign-original", "another-chat"
	foreign.Message.ClientMessageID = "00000000-0000-4000-8000-000000000009"
	if _, err := collections.timeline.InsertOne(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"missing", "foreign-original"} {
		params := integrationCreateMessageParams(t, "invalid-reply", "invalid-event", "Resposta inválida")
		clientID, _ := timeline.ParseClientMessageID("00000000-0000-4000-8000-000000000008")
		params.Message, _ = timeline.NewMessage(clientID, params.Message.SenderID(), params.Message.Content())
		replyID, _ := timeline.ParseID(target)
		params.Message = params.Message.WithReply(replyID, nil)
		if _, err := store.CreateWithOutbox(ctx, params); !errors.Is(err, ports.ErrReplyUnavailable) {
			t.Fatalf("accepted %s: %v", target, err)
		}
	}
	count, err := collections.timeline.CountDocuments(ctx, bson.M{"chat_id": original.ChatID.String()})
	if err != nil || count != 3 {
		t.Fatalf("invalid reply persisted: %d %v", count, err)
	}
	assertIntegrationCount(t, ctx, collections.outbox, 3)
}

func assertReplySnapshot(t *testing.T, item *timeline.Item, original ports.CreateMessageParams) {
	t.Helper()
	reply := item.Message().ReplyTo()
	if item.Message().ReplyID() != original.MessageID || reply == nil || reply.SenderID != original.Message.SenderID() || reply.Sequence.Int64() != 1 || reply.Content.Value() != original.Message.Content().Value() {
		t.Fatalf("reply snapshot missing: %+v", reply)
	}
}
