package timelinerepository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	applicationtimeline "github.com/wendryuslima/nexus-services/internal/application/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/search"
	"github.com/wendryuslima/nexus-services/internal/ports"
	chatrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/chat_repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestSearchAndContextAcrossUnloadedHistory(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database(fmt.Sprintf("nexus_search_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = db.Drop(cleanup)
		_ = client.Disconnect(cleanup)
	})
	repository, _ := NewRepository(db.Collection("timeline_items"))
	if err := repository.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	chatID, _ := chat.ParseID("chat-1")
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = db.Collection("chats").InsertOne(ctx, bson.M{"_id": "chat-1", "participant_one_id": "user-1", "participant_two_id": "user-2", "participant_ids": bson.A{"user-1", "user-2"}, "last_sequence": 1000, "created_at": now, "updated_at": now, "summary_sort_at": now})
	if err != nil {
		t.Fatal(err)
	}
	document := func(sequence int64, text string) bson.M {
		return bson.M{"_id": fmt.Sprintf("message-%d", sequence), "chat_id": "chat-1", "sequence": sequence, "kind": "MESSAGE", "created_at": now, "updated_at": now,
			"message": bson.M{"client_message_id": fmt.Sprintf("018f92d4-2c47-7c87-b33e-%012x", sequence), "sender_id": "user-1", "content": bson.M{"type": "TEXT", "value": text}}}
	}
	documents := make([]any, 0, 1000)
	for sequence := int64(1); sequence <= 1000; sequence++ {
		text := "Mensagem comum"
		if sequence%100 == 0 {
			text = "Contrato AÇÃO atualizado"
		}
		if sequence == 501 {
			text = "Contratos em ação"
		}
		doc := document(sequence, text)
		if sequence == 550 {
			doc["message"].(bson.M)["content"] = bson.M{"type": "IMAGE", "image_id": "image-1", "caption": "Contrato ação especial"}
		}
		documents = append(documents, doc)
	}
	if _, err := repository.collection.InsertMany(ctx, documents); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repository.BackfillSearchWords(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.CheckRealtimeData(ctx); err != nil {
		t.Fatal(err)
	}
	words := search.Words("contrato ação")
	params := ports.ListTimelineParams{ChatID: chatID, Limit: 3, SequenceOrder: true, SearchWords: words}
	page, err := repository.ListByChat(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.Items[0].Sequence().Int64() != 800 || !page.HasNext {
		t.Fatalf("unexpected first page: %+v", page)
	}
	newDoc := document(1001, "Contrato ação novo")
	newDoc["search_words"] = words
	if _, err := repository.collection.InsertOne(ctx, newDoc); err != nil {
		t.Fatal(err)
	}
	params.BeforeSequence, params.ThroughSequence = page.NextBeforeSequence, page.SnapshotThroughSequence
	seen := 3
	foundCaption := false
	for {
		page, err = repository.ListByChat(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			sequence := item.Sequence().Int64()
			if sequence == 501 || sequence == 1001 {
				t.Fatalf("substring or post-snapshot item leaked: %d", sequence)
			}
			foundCaption = foundCaption || sequence == 550
		}
		seen += len(page.Items)
		if !page.HasNext {
			break
		}
		params.BeforeSequence = page.NextBeforeSequence
	}
	if seen != 11 || !foundCaption {
		t.Fatalf("got %d matches, caption=%v", seen, foundCaption)
	}
	anchor := int64(100)
	contextPage, err := repository.ListByChat(ctx, ports.ListTimelineParams{ChatID: chatID, Limit: 51, SequenceOrder: true, AroundSequence: &anchor})
	if err != nil {
		t.Fatal(err)
	}
	if len(contextPage.Items) != 51 || contextPage.Items[0].Sequence().Int64() != 75 || contextPage.Items[50].Sequence().Int64() != 125 || !contextPage.HasOlder || !contextPage.HasNewer {
		t.Fatalf("unexpected context: %+v", contextPage)
	}
	for _, boundary := range []int64{1, 1001} {
		page, err := repository.ListByChat(ctx, ports.ListTimelineParams{ChatID: chatID, Limit: 51, SequenceOrder: true, AroundSequence: &boundary})
		if err != nil {
			t.Fatal(err)
		}
		if boundary == 1 && page.HasOlder || boundary == 1001 && page.HasNewer {
			t.Fatalf("incorrect boundary: %+v", page)
		}
	}
	chats, _ := chatrepository.NewRepository(db.Collection("chats"))
	useCase, _ := applicationtimeline.NewListUseCase(applicationtimeline.ListDependencies{ChatRepository: chats, TimelineRepository: repository})
	if _, err := useCase.Execute(ctx, applicationtimeline.ListInput{CurrentUserID: "outsider", ChatID: "chat-1", Query: "contrato", SequenceOrder: true}); err != applicationtimeline.ErrChatNotFound {
		t.Fatalf("unauthorized search: %v", err)
	}
	if _, err := useCase.Execute(ctx, applicationtimeline.ListInput{CurrentUserID: "outsider", ChatID: "chat-1", AroundSequence: &anchor, SequenceOrder: true}); err != applicationtimeline.ErrChatNotFound {
		t.Fatalf("unauthorized context: %v", err)
	}
	// Check the real query planner, including a deep cursor: no collection scan or blocking sort.
	var explain struct {
		QueryPlanner struct {
			WinningPlan bson.M `bson:"winningPlan"`
		} `bson:"queryPlanner"`
		ExecutionStats struct {
			TotalDocsExamined int64 `bson:"totalDocsExamined"`
		} `bson:"executionStats"`
	}
	err = db.RunCommand(ctx, bson.D{{Key: "explain", Value: bson.D{
		{Key: "find", Value: "timeline_items"}, {Key: "filter", Value: bson.M{"chat_id": "chat-1", "search_words": "contrato", "sequence": bson.M{"$lt": 500}}},
		{Key: "sort", Value: bson.M{"sequence": -1}}, {Key: "limit", Value: 3},
	}}, {Key: "verbosity", Value: "executionStats"}}).Decode(&explain)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := bson.MarshalExtJSON(explain.QueryPlanner.WinningPlan, false, false)
	if strings.Contains(string(plan), `"stage":"COLLSCAN"`) || strings.Contains(string(plan), `"stage":"SORT"`) {
		t.Fatalf("unindexed search plan: %s", plan)
	}
	if explain.ExecutionStats.TotalDocsExamined > 4 {
		t.Fatalf("deep search examined too many documents: %d", explain.ExecutionStats.TotalDocsExamined)
	}
}
