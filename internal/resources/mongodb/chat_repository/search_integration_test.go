package chatrepository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	applicationchats "github.com/wendryuslima/nexus-services/internal/application/chats"
	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/search"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	userrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/user_repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestParticipantSearchBeforePaginationAndDirectAccess(t *testing.T) {
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
	db := client.Database(fmt.Sprintf("nexus_chat_search_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = db.Drop(cleanup)
		_ = client.Disconnect(cleanup)
	})
	repository, _ := NewRepository(db.Collection("chats"))
	if err := repository.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	owner, _ := user.ParseID("owner")
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "owner", "email": "ana.owner@example.com", "password_hash": "hash", "created_at": now})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 80 {
		// Exercise both participant positions in the canonical direct-chat pair.
		prefix := "peer"
		if i%2 == 0 {
			prefix = "aaa"
		}
		peer, _ := user.ParseID(fmt.Sprintf("%s-%03d", prefix, i))
		email := fmt.Sprintf("bruno.%03d@example.com", i)
		if i%3 == 0 {
			email = fmt.Sprintf("ana.silva.%03d@example.com", i)
		}
		_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": peer.String(), "email": email, "password_hash": "hash", "created_at": now})
		if err != nil {
			t.Fatal(err)
		}
		id, _ := chat.ParseID(fmt.Sprintf("chat-%03d", i))
		candidate, _ := chat.NewDirect(id, owner, peer, now)
		if _, _, err := repository.GetOrCreateDirect(ctx, candidate); err != nil {
			t.Fatal(err)
		}
	}
	// A legacy projection must be backfilled, including the inverse perspective.
	if _, err := repository.collection.UpdateMany(ctx, bson.M{}, bson.M{"$unset": bson.M{"participant_one_words": "", "participant_two_words": ""}}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repository.BackfillSearchWords(ctx); err != nil {
			t.Fatal(err)
		}
	}
	params := ports.ListChatsParams{ParticipantID: owner, Limit: 5, SearchWords: search.Words("ana silva")}
	seen := map[string]bool{}
	for {
		page, err := repository.ListByParticipant(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		for _, conversation := range page.Chats {
			id := conversation.ID().String()
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
		}
		if !page.HasNext {
			break
		}
		params.After = page.NextCursor
	}
	if len(seen) != 27 {
		t.Fatalf("got %d matching chats; want 27", len(seen))
	}
	var explain struct {
		QueryPlanner struct {
			WinningPlan bson.M `bson:"winningPlan"`
		} `bson:"queryPlanner"`
	}
	if err := db.RunCommand(ctx, bson.D{{Key: "explain", Value: bson.D{
		{Key: "find", Value: "chats"}, {Key: "filter", Value: searchChatsFilter(params)},
		{Key: "sort", Value: bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}}, {Key: "limit", Value: 5},
	}}}).Decode(&explain); err != nil {
		t.Fatal(err)
	}
	plan, _ := bson.MarshalExtJSON(explain.QueryPlanner.WinningPlan, false, false)
	if strings.Contains(string(plan), `"stage":"COLLSCAN"`) || strings.Contains(string(plan), `"stage":"SORT"`) {
		t.Fatalf("unindexed conversation search: %s", plan)
	}
	params.After, params.SearchWords = nil, search.Words("an")
	page, err := repository.ListByParticipant(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Chats) != 0 {
		t.Fatal("partial participant word matched")
	}
	params.SearchWords = search.Words("ana")
	page, err = repository.ListByParticipant(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Chats) != 5 {
		t.Fatalf("first filtered page: %+v", page)
	}
	outsider, _ := user.ParseID("outsider")
	params.ParticipantID = outsider
	page, err = repository.ListByParticipant(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Chats) != 0 {
		t.Fatal("outsider can search conversations")
	}
	users, _ := userrepository.NewRepository(db.Collection("users"))
	useCase, _ := applicationchats.NewListUseCase(repository, users)
	// Fetch a chat directly without loading the sidebar pages.
	detail, err := useCase.Execute(ctx, applicationchats.ListInput{CurrentUserID: "owner", ChatID: "chat-000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Chats) != 1 || detail.Chats[0].RelatedUserEmail != "ana.silva.000@example.com" {
		t.Fatalf("invalid direct detail: %+v", detail)
	}
	if _, err := useCase.Execute(ctx, applicationchats.ListInput{CurrentUserID: "outsider", ChatID: "chat-000"}); err != ports.ErrChatNotFound {
		t.Fatalf("unauthorized detail: %v", err)
	}
	if _, err := useCase.Execute(ctx, applicationchats.ListInput{CurrentUserID: "owner", ChatID: "missing"}); err != ports.ErrChatNotFound {
		t.Fatalf("missing detail: %v", err)
	}
}
