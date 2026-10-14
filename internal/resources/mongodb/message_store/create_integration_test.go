package messagestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
	resourcemongodb "github.com/wendryuslima/nexus-services/internal/resources/mongodb"
	timelinerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/timeline_repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const mongoDBTestURIEnvironment = "MONGODB_TEST_URI"

func TestCreateWithOutboxPersistsAtomicallyAndIsIdempotent(t *testing.T) {
	uri := os.Getenv(mongoDBTestURIEnvironment)
	if uri == "" {
		t.Skipf("set %s to run MongoDB integration tests", mongoDBTestURIEnvironment)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, collections, cleanup := newMessageStoreIntegrationFixture(t, ctx, uri)
	defer cleanup()

	params := integrationCreateMessageParams(t, "message-1", "event-1", "Olá!")
	insertIntegrationChat(t, ctx, collections.chats, params)

	firstResult, err := store.CreateWithOutbox(ctx, params)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	if !firstResult.Created || firstResult.Item.Sequence().Int64() != 1 {
		t.Fatalf("unexpected first result: %+v", firstResult)
	}

	secondResult, err := store.CreateWithOutbox(ctx, params)
	if err != nil {
		t.Fatalf("retry message creation: %v", err)
	}
	if secondResult.Created || secondResult.Item.ID().String() != "message-1" {
		t.Fatalf("unexpected idempotent retry result: %+v", secondResult)
	}

	assertIntegrationCount(t, ctx, collections.timeline, 1)
	assertIntegrationCount(t, ctx, collections.outbox, 1)

	var storedChat struct {
		LastSequence int64 `bson:"last_sequence"`
	}
	if err := collections.chats.FindOne(ctx, bson.D{{Key: "_id", Value: params.ChatID.String()}}).Decode(&storedChat); err != nil {
		t.Fatalf("read chat sequence: %v", err)
	}
	if storedChat.LastSequence != 1 {
		t.Fatalf("expected last_sequence 1, got %d", storedChat.LastSequence)
	}

	conflictingParams := integrationCreateMessageParams(t, "message-2", "event-2", "Conteúdo diferente")
	conflictingParams.ChatID = params.ChatID
	conflictingParams.Message = params.Message
	conflictingContent, err := timeline.NewMessageContent(timeline.ContentTypeText, "Conteúdo diferente")
	if err != nil {
		t.Fatalf("create conflicting content: %v", err)
	}
	conflictingParams.Message, err = timeline.NewMessage(
		params.Message.ClientMessageID(),
		params.Message.SenderID(),
		conflictingContent,
	)
	if err != nil {
		t.Fatalf("create conflicting message: %v", err)
	}

	_, err = store.CreateWithOutbox(ctx, conflictingParams)
	if !errors.Is(err, ports.ErrMessageIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
	assertIntegrationCount(t, ctx, collections.timeline, 1)
	assertIntegrationCount(t, ctx, collections.outbox, 1)
}

func TestCreateWithOutboxRollsBackWhenTheOutboxInsertFails(t *testing.T) {
	uri := os.Getenv(mongoDBTestURIEnvironment)
	if uri == "" {
		t.Skipf("set %s to run MongoDB integration tests", mongoDBTestURIEnvironment)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, collections, cleanup := newMessageStoreIntegrationFixture(t, ctx, uri)
	defer cleanup()

	params := integrationCreateMessageParams(t, "message-1", "event-1", "Olá!")
	insertIntegrationChat(t, ctx, collections.chats, params)
	if _, err := collections.outbox.InsertOne(ctx, bson.D{{Key: "_id", Value: params.EventID}}); err != nil {
		t.Fatalf("seed conflicting outbox event: %v", err)
	}

	if _, err := store.CreateWithOutbox(ctx, params); err == nil {
		t.Fatal("expected the duplicate outbox ID to abort the transaction")
	}
	assertIntegrationCount(t, ctx, collections.timeline, 0)

	var storedChat struct {
		LastSequence int64 `bson:"last_sequence"`
	}
	if err := collections.chats.FindOne(ctx, bson.D{{Key: "_id", Value: params.ChatID.String()}}).Decode(&storedChat); err != nil {
		t.Fatalf("read chat sequence: %v", err)
	}
	if storedChat.LastSequence != 0 {
		t.Fatalf("expected transaction rollback to retain sequence 0, got %d", storedChat.LastSequence)
	}
}

type messageStoreIntegrationCollections struct {
	chats    *mongo.Collection
	timeline *mongo.Collection
	outbox   *mongo.Collection
}

func newMessageStoreIntegrationFixture(
	t *testing.T,
	ctx context.Context,
	uri string,
) (*Repository, messageStoreIntegrationCollections, func()) {
	t.Helper()

	databaseName := fmt.Sprintf("nexus_message_store_test_%d", time.Now().UnixNano())
	client, err := resourcemongodb.Connect(ctx, config.MongoDBConfig{
		URI:              uri,
		Database:         databaseName,
		ConnectTimeout:   10 * time.Second,
		OperationTimeout: 10 * time.Second,
		ShutdownTimeout:  10 * time.Second,
	})
	if err != nil {
		t.Fatalf("connect MongoDB: %v", err)
	}

	collections := messageStoreIntegrationCollections{}
	collections.chats, err = client.Collection("chats")
	if err != nil {
		t.Fatalf("get chats collection: %v", err)
	}
	collections.timeline, err = client.Collection("timeline_items")
	if err != nil {
		t.Fatalf("get timeline collection: %v", err)
	}
	collections.outbox, err = client.Collection("outbox_events")
	if err != nil {
		t.Fatalf("get outbox collection: %v", err)
	}

	timelineRepository, err := timelinerepository.NewRepository(collections.timeline)
	if err != nil {
		t.Fatalf("create timeline repository: %v", err)
	}
	if err := timelineRepository.EnsureIndexes(ctx); err != nil {
		t.Fatalf("create timeline indexes: %v", err)
	}

	store, err := NewRepository(client, collections.chats, collections.timeline, collections.outbox)
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}

	cleanup := func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, collection := range []*mongo.Collection{collections.chats, collections.timeline, collections.outbox} {
			if _, err := collection.DeleteMany(cleanupContext, bson.D{}); err != nil {
				t.Errorf("clean %s collection: %v", collection.Name(), err)
			}
		}
		if err := client.Close(cleanupContext); err != nil {
			t.Errorf("close MongoDB client: %v", err)
		}
	}

	return store, collections, cleanup
}

func integrationCreateMessageParams(
	t *testing.T,
	messageIDValue string,
	eventID string,
	contentValue string,
) ports.CreateMessageParams {
	t.Helper()

	messageID, err := timeline.ParseID(messageIDValue)
	if err != nil {
		t.Fatalf("parse message ID: %v", err)
	}
	chatID, err := chat.ParseID("chat-1")
	if err != nil {
		t.Fatalf("parse chat ID: %v", err)
	}
	senderID, err := user.ParseID("user-1")
	if err != nil {
		t.Fatalf("parse sender ID: %v", err)
	}
	clientMessageID, err := timeline.ParseClientMessageID("018f92d4-2c47-7c87-b33e-d2ea331d2ff2")
	if err != nil {
		t.Fatalf("parse client message ID: %v", err)
	}
	content, err := timeline.NewMessageContent(timeline.ContentTypeText, contentValue)
	if err != nil {
		t.Fatalf("create message content: %v", err)
	}
	message, err := timeline.NewMessage(clientMessageID, senderID, content)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	return ports.CreateMessageParams{
		MessageID: messageID,
		EventID:   eventID,
		ChatID:    chatID,
		Message:   message,
		CreatedAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
	}
}

func insertIntegrationChat(
	t *testing.T,
	ctx context.Context,
	collection *mongo.Collection,
	params ports.CreateMessageParams,
) {
	t.Helper()

	if _, err := collection.InsertOne(ctx, bson.D{
		{Key: "_id", Value: params.ChatID.String()},
		{Key: "participant_ids", Value: bson.A{params.Message.SenderID().String(), "user-2"}},
		{Key: "last_sequence", Value: int64(0)},
		{Key: "summary_sort_at", Value: params.CreatedAt.Add(-time.Hour)},
	}); err != nil {
		t.Fatalf("insert chat: %v", err)
	}
}

func assertIntegrationCount(
	t *testing.T,
	ctx context.Context,
	collection *mongo.Collection,
	want int64,
) {
	t.Helper()

	count, err := collection.CountDocuments(ctx, bson.D{}, options.Count())
	if err != nil {
		t.Fatalf("count %s documents: %v", collection.Name(), err)
	}
	if count != want {
		t.Fatalf("expected %d documents in %s, got %d", want, collection.Name(), count)
	}
}
