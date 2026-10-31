package messagestore

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/ports"
	imagerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/image_repository"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestImageMessageAtomicAttachmentAndConcurrentRetries(t *testing.T) {
	uri := os.Getenv(mongoDBTestURIEnvironment)
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	store, c, cleanup := newMessageStoreIntegrationFixture(t, ctx, uri)
	defer cleanup()
	params := integrationCreateMessageParams(t, "image-message", "image-event", "text")
	params.CreatedAt = time.Now().UTC()
	insertIntegrationChat(t, ctx, c.chats, params)
	r, _ := imagerepository.New(c.images)
	if err := r.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	asset := domainimage.Asset{ID: "image-1", UploadedBy: params.Message.SenderID().String(), ChatID: params.ChatID.String(), ClientUploadID: "upload-1", Bucket: "private", ObjectKey: "images/image-1", Checksum: strings.Repeat("a", 64), ContentType: "image/png", SizeBytes: 20, Width: 2, Height: 2, Status: domainimage.Pending, UploadToken: "lease-1", CreatedAt: params.CreatedAt, ExpiresAt: params.CreatedAt.Add(time.Hour)}
	if _, owned, err := r.ReserveUpload(ctx, asset, params.CreatedAt); err != nil || !owned {
		t.Fatalf("reserve: %v", err)
	}
	if ok, err := r.MarkReady(ctx, asset.ID, asset.UploadToken, params.CreatedAt); err != nil || !ok {
		t.Fatalf("ready: %v", err)
	}
	content, _ := timeline.NewImageContent(asset.ID, "Legenda 🙂")
	params.Message, _ = timeline.NewMessage(params.Message.ClientMessageID(), params.Message.SenderID(), content)
	_, err := c.outbox.InsertOne(ctx, bson.M{"_id": params.EventID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWithOutbox(ctx, params); err == nil {
		t.Fatal("expected forced duplicate outbox failure")
	}
	stored, err := r.FindByID(ctx, asset.ID)
	if err != nil || stored.Status != domainimage.Ready {
		t.Fatalf("attachment did not roll back: %+v %v", stored, err)
	}
	_, _ = c.outbox.DeleteOne(ctx, bson.M{"_id": params.EventID})
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			result, err := store.CreateWithOutbox(ctx, params)
			if err == nil && (result.Item.Message().Content().ImageID() != asset.ID || result.Item.Message().Content().Caption() != "Legenda 🙂") {
				err = errors.New("content was lost")
			}
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertIntegrationCount(t, ctx, c.timeline, 1)
	assertIntegrationCount(t, ctx, c.outbox, 1)
	stored, err = r.FindByID(ctx, asset.ID)
	if err != nil || stored.Status != domainimage.Attached || stored.MessageID != "image-message" {
		t.Fatalf("attachment: %+v %v", stored, err)
	}
	other := params
	other.MessageID, _ = timeline.ParseID("other-message")
	other.EventID = "other-event"
	clientID, _ := timeline.ParseClientMessageID("00000000-0000-4000-8000-000000000002")
	other.Message, _ = timeline.NewMessage(clientID, params.Message.SenderID(), content)
	if _, err := store.CreateWithOutbox(ctx, other); !errors.Is(err, ports.ErrImageUnavailable) {
		t.Fatalf("reused attached image: %v", err)
	}
	if claimed, err := r.ClaimAbandoned(ctx, params.CreatedAt.Add(48*time.Hour), "delete"); err != nil || claimed != nil {
		t.Fatalf("attached image claimed for deletion: %+v %v", claimed, err)
	}
}

func TestImageUploadLeasesAndDeletionRecovery(t *testing.T) {
	uri := os.Getenv(mongoDBTestURIEnvironment)
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, c, cleanup := newMessageStoreIntegrationFixture(t, ctx, uri)
	defer cleanup()
	r, _ := imagerepository.New(c.images)
	if err := r.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a := domainimage.Asset{ID: "abandoned", UploadedBy: "sender", ChatID: "chat", ClientUploadID: "upload", Checksum: strings.Repeat("a", 64), SizeBytes: 20, UploadToken: "first", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if _, owned, err := r.ReserveUpload(ctx, a, now); err != nil || !owned {
		t.Fatal(err)
	}
	a.ID = "candidate-2"
	a.UploadToken = "second"
	if _, owned, err := r.ReserveUpload(ctx, a, now); err != nil || owned {
		t.Fatalf("stole active upload: %v", err)
	}
	if err := r.ReleaseUpload(ctx, "abandoned", "wrong-token"); err != nil {
		t.Fatal(err)
	}
	if _, owned, err := r.ReserveUpload(ctx, a, now); err != nil || owned {
		t.Fatalf("wrong lease released upload: %v", err)
	}
	if err := r.ReleaseUpload(ctx, "abandoned", "first"); err != nil {
		t.Fatal(err)
	}
	if current, owned, err := r.ReserveUpload(ctx, a, now); err != nil || !owned || current.ID != "abandoned" {
		t.Fatalf("retry did not reuse identity: %+v %v", current, err)
	}
	if ok, err := r.MarkReady(ctx, "abandoned", "first", now); err != nil || ok {
		t.Fatalf("stale lease marked ready: %v", err)
	}
	changed := a
	changed.Checksum = strings.Repeat("b", 64)
	if _, _, err := r.ReserveUpload(ctx, changed, now); !errors.Is(err, ports.ErrImageUploadConflict) {
		t.Fatalf("changed bytes accepted: %v", err)
	}
	future := now.Add(2 * time.Hour)
	claimed, err := r.ClaimAbandoned(ctx, future, "delete-1")
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	if ok, err := r.MarkReady(ctx, "abandoned", "second", future); err != nil || ok {
		t.Fatal("late upload resurrected deleting image")
	}
	if err := r.CompleteDeletion(ctx, *claimed, future); err != nil {
		t.Fatal(err)
	}
	if next, err := r.ClaimAbandoned(ctx, future.Add(time.Minute), "too-early"); err != nil || next != nil {
		t.Fatalf("deletion grace ignored: %v", err)
	}
	claimed, err = r.ClaimAbandoned(ctx, future.Add(domainimage.DeletionGrace), "delete-2")
	if err != nil || claimed == nil {
		t.Fatalf("second deletion not scheduled: %v", err)
	}
	if err := r.CompleteDeletion(ctx, *claimed, future.Add(domainimage.DeletionGrace)); err != nil {
		t.Fatal(err)
	}
	stored, err := r.FindByID(ctx, "abandoned")
	if err != nil || stored.Status != domainimage.Deleted {
		t.Fatalf("tombstone: %+v %v", stored, err)
	}
	if _, _, err := r.ReserveUpload(ctx, a, future); !errors.Is(err, ports.ErrImageExpired) {
		t.Fatalf("expired identity was resurrected: %v", err)
	}
}
