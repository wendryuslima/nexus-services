package chatrepository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestUnreadCountersAndMonotonicParticipantReads(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database(fmt.Sprintf("nexus_unread_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = db.Drop(cleanup)
		_ = client.Disconnect(cleanup)
	})
	chats, items := db.Collection("chats"), db.Collection("timeline_items")
	r, _ := NewReadRepository(chats, items)
	id, _ := chat.ParseID("chat")
	alice, _ := user.ParseID("alice")
	bob, _ := user.ParseID("bob")
	stranger, _ := user.ParseID("stranger")
	// Existing conversations have no read fields and still count historical messages.
	_, err = chats.InsertOne(ctx, bson.M{"_id": "chat", "participant_ids": bson.A{"alice", "bob"}, "participant_one_id": "alice", "participant_two_id": "bob", "last_sequence": 4})
	if err != nil {
		t.Fatal(err)
	}
	for i, sender := range []string{"alice", "bob", "alice", "bob"} {
		_, err := items.InsertOne(ctx, bson.M{"chat_id": "chat", "sequence": i + 1, "kind": "MESSAGE", "message": bson.M{"sender_id": sender}})
		if err != nil {
			t.Fatal(err)
		}
	}
	assertState := func(participant user.ID, unread, read, sequence int64) {
		t.Helper()
		states, err := r.ReadStates(ctx, []chat.ID{id}, participant)
		if err != nil {
			t.Fatal(err)
		}
		s := states[id.String()]
		if s.UnreadCount != unread || s.LastReadSequence != read || s.LastSequence != sequence {
			t.Fatalf("unexpected state for %s: %+v", participant.String(), s)
		}
	}
	assertState(alice, 2, 0, 4)
	assertState(bob, 2, 0, 4)
	if _, err := r.MarkRead(ctx, id, alice, 3); err != nil {
		t.Fatal(err)
	}
	assertState(alice, 1, 3, 4)
	assertState(bob, 2, 0, 4)
	if _, err := r.MarkRead(ctx, id, alice, 1); err != nil {
		t.Fatal(err)
	}
	assertState(alice, 1, 3, 4)
	if _, err := r.MarkRead(ctx, id, bob, 999); err != nil {
		t.Fatal(err)
	}
	assertState(bob, 0, 4, 4)
	if _, err := r.MarkRead(ctx, id, stranger, 4); !errors.Is(err, ports.ErrChatNotFound) {
		t.Fatalf("foreign read allowed: %v", err)
	}
	if _, err := r.MarkRead(ctx, id, alice, -1); !errors.Is(err, ports.ErrInvalidReadSequence) {
		t.Fatalf("negative sequence allowed: %v", err)
	}
	// A message arriving after the acknowledged boundary must remain unread.
	_, err = items.InsertOne(ctx, bson.M{"chat_id": "chat", "sequence": 5, "kind": "MESSAGE", "message": bson.M{"sender_id": "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = chats.UpdateOne(ctx, bson.M{"_id": "chat"}, bson.M{"$set": bson.M{"last_sequence": 5}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.MarkRead(ctx, id, bob, 4); err != nil {
		t.Fatal(err)
	}
	assertState(bob, 1, 4, 5)
}
