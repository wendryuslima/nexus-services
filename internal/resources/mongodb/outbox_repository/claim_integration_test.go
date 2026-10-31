package outboxrepository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func outboxFixture(t *testing.T, monitor *event.CommandMonitor) (context.Context, *Repository) {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI to run MongoDB integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetMonitor(monitor).SetTimeout(10 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	database := client.Database(fmt.Sprintf("nexus_outbox_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()
		if err := database.Drop(cleanupCtx); err != nil {
			t.Error(err)
		}
		if err := client.Disconnect(cleanupCtx); err != nil {
			t.Error(err)
		}
	})
	repository, err := NewRepository(database.Collection("outbox_events"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	return ctx, repository
}

func seedOutbox(t *testing.T, ctx context.Context, repository *Repository, documents []any) {
	t.Helper()
	if _, err := repository.collection.InsertMany(ctx, documents); err != nil {
		t.Fatal(err)
	}
}

func pendingEvent(id string, now time.Time) bson.M {
	return bson.M{"_id": id, "status": statusPending, "published_at": nil,
		"next_attempt_at": now, "created_at": now, "locked_until": nil}
}

func processingEvent(id string, now, expires time.Time) bson.M {
	doc := pendingEvent(id, now)
	doc["status"], doc["locked_until"], doc["locked_by"], doc["lease_id"] = statusProcessing, expires, "old-worker", "old-"+id
	return doc
}

func TestConcurrentClaimsOnlyReserveEligiblePendingEventsOnce(t *testing.T) {
	ctx, repository := outboxFixture(t, nil)
	now := time.Now().UTC().Truncate(time.Millisecond)
	var documents []any
	const pending = 128
	for i := range pending {
		documents = append(documents, pendingEvent(fmt.Sprintf("pending-%03d", i), now))
	}
	for i := range 8 {
		documents = append(documents,
			pendingEvent(fmt.Sprintf("future-%d", i), now.Add(time.Hour)),
			processingEvent(fmt.Sprintf("active-%d", i), now, now.Add(time.Hour)),
			processingEvent(fmt.Sprintf("expired-%d", i), now, now.Add(-time.Second)),
			bson.M{"_id": fmt.Sprintf("published-%d", i), "status": statusPublished, "next_attempt_at": now, "published_at": now},
		)
	}
	seedOutbox(t, ctx, repository, documents)
	var seen sync.Map
	var count atomic.Int32
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for attempt := 0; ; attempt++ {
				lease := fmt.Sprintf("lease-%d-%d", worker, attempt)
				claimed, err := repository.ClaimNext(ctx, fmt.Sprintf("worker-%d", worker), lease, now, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if claimed == nil {
					return
				}
				if claimed.LeaseID != lease {
					t.Errorf("wrong lease: %+v", claimed)
				}
				if _, duplicate := seen.LoadOrStore(claimed.ID, true); duplicate {
					t.Errorf("event claimed twice: %s", claimed.ID)
				}
				count.Add(1)
			}
		})
	}
	workers.Wait()
	if count.Load() != pending {
		t.Fatalf("claimed %d events, want %d", count.Load(), pending)
	}
}

func TestRecoveryIsBoundedPreservesActiveLeasesAndFencesOldOwner(t *testing.T) {
	ctx, repository := outboxFixture(t, nil)
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedOutbox(t, ctx, repository, []any{
		processingEvent("expired-1", now, now.Add(-3*time.Second)),
		processingEvent("expired-2", now, now.Add(-2*time.Second)),
		processingEvent("expired-3", now, now.Add(-time.Second)),
		processingEvent("active", now, now.Add(time.Hour)),
		bson.M{"_id": "published", "status": statusPublished, "locked_until": now.Add(-time.Hour), "published_at": now},
		pendingEvent("retry-later", now.Add(time.Hour)),
	})
	recovered, err := repository.RecoverExpired(ctx, now, 2)
	if err != nil || recovered != 2 {
		t.Fatalf("recovered=%d error=%v", recovered, err)
	}
	claimed, err := repository.ClaimNext(ctx, "new-worker", "new-lease", now, time.Minute)
	if err != nil || claimed == nil || claimed.ID != "expired-1" {
		t.Fatalf("claim=%+v error=%v", claimed, err)
	}
	receipt := ports.PublishReceipt{Stream: "CHAT_EVENTS", Sequence: 1}
	if updated, err := repository.MarkPublished(ctx, claimed.ID, "old-expired-1", receipt, now); err != nil || updated {
		t.Fatalf("old owner could finish recovered event: updated=%v error=%v", updated, err)
	}
	if updated, err := repository.ScheduleRetry(ctx, claimed.ID, "old-expired-1", now, "failure"); err != nil || updated {
		t.Fatalf("old owner could retry recovered event: updated=%v error=%v", updated, err)
	}
	if updated, err := repository.MarkPublished(ctx, claimed.ID, "new-lease", receipt, now); err != nil || !updated {
		t.Fatalf("new owner could not finish: updated=%v error=%v", updated, err)
	}
	if recovered, err = repository.RecoverExpired(ctx, now, 2); err != nil || recovered != 1 {
		t.Fatalf("second recovery=%d error=%v", recovered, err)
	}
	if recovered, err = repository.RecoverExpired(ctx, now, 2); err != nil || recovered != 0 {
		t.Fatalf("active or published event recovered: count=%d error=%v", recovered, err)
	}
}

func TestDelayedRecoveryCannotResetANewerLease(t *testing.T) {
	var armed atomic.Bool
	updateStarted, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	monitor := &event.CommandMonitor{Started: func(ctx context.Context, command *event.CommandStartedEvent) {
		if command.CommandName == "update" && armed.CompareAndSwap(true, false) {
			close(updateStarted)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}}
	ctx, repository := outboxFixture(t, monitor)
	now := time.Now().UTC().Truncate(time.Millisecond)
	seedOutbox(t, ctx, repository, []any{processingEvent("expired", now, now.Add(-time.Second))})
	armed.Store(true)
	type result struct {
		recovered int64
		err       error
	}
	done := make(chan result, 1)
	go func() { n, err := repository.RecoverExpired(ctx, now, 1); done <- result{n, err} }()
	select {
	case <-updateStarted:
	case <-ctx.Done():
		t.Fatal("recovery did not reach update")
	}
	if n, err := repository.RecoverExpired(ctx, now, 1); err != nil || n != 1 {
		t.Fatalf("second recovery=%d error=%v", n, err)
	}
	claimed, err := repository.ClaimNext(ctx, "new-worker", "new-lease", now, time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("claim=%+v error=%v", claimed, err)
	}
	once.Do(func() { close(release) })
	select {
	case r := <-done:
		if r.err != nil || r.recovered != 0 {
			t.Fatalf("stale recovery=%d error=%v", r.recovered, r.err)
		}
	case <-ctx.Done():
		t.Fatal("delayed recovery did not complete")
	}
	if n, err := repository.collection.CountDocuments(ctx, bson.M{"_id": "expired", "status": statusProcessing, "lease_id": "new-lease"}); err != nil || n != 1 {
		t.Fatalf("new lease overwritten: count=%d error=%v", n, err)
	}
}

func TestClaimPlanUsesPendingIndexWithoutSortingOrScanningBacklog(t *testing.T) {
	ctx, repository := outboxFixture(t, nil)
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err := repository.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "status", Value: 1}, {Key: "next_attempt_at", Value: 1}, {Key: "created_at", Value: 1}},
		Options: options.Index().SetName("outbox_status_schedule"),
	})
	if err != nil {
		t.Fatal(err)
	}
	documents := make([]any, 0, 3000)
	for i := range 2000 {
		documents = append(documents, pendingEvent(fmt.Sprintf("pending-%04d", i), now))
	}
	for i := range 1000 {
		documents = append(documents, processingEvent(fmt.Sprintf("active-%04d", i), now, now.Add(time.Hour)))
	}
	seedOutbox(t, ctx, repository, documents)
	var explanation struct {
		QueryPlanner struct {
			WinningPlan bson.M `bson:"winningPlan"`
		} `bson:"queryPlanner"`
		ExecutionStats struct {
			DocsExamined int64 `bson:"totalDocsExamined"`
			KeysExamined int64 `bson:"totalKeysExamined"`
		} `bson:"executionStats"`
	}
	err = repository.collection.Database().RunCommand(ctx, bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "findAndModify", Value: repository.collection.Name()},
			{Key: "query", Value: bson.M{"status": statusPending, "published_at": nil, "next_attempt_at": bson.M{"$lte": now}}},
			{Key: "sort", Value: bson.D{{Key: "next_attempt_at", Value: 1}, {Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}},
			{Key: "update", Value: bson.M{"$set": bson.M{"status": statusProcessing, "lease_id": "explain", "locked_until": now.Add(time.Minute)}}},
			{Key: "new", Value: true},
		}},
		{Key: "verbosity", Value: "executionStats"},
	}).Decode(&explanation)
	if err != nil {
		t.Fatal(err)
	}
	stats := explanation.ExecutionStats
	plan := explanation.QueryPlanner.WinningPlan
	if planHas(plan, "stage", "SORT") || !planHas(plan, "indexName", outboxPendingScheduleIndexName) {
		t.Fatalf("unexpected winning plan: %v", plan)
	}
	for key, n := range map[string]int64{"totalDocsExamined": stats.DocsExamined, "totalKeysExamined": stats.KeysExamined} {
		if n < 1 || n > 2 {
			t.Fatalf("%s=%d, expected at most 2 for 2000 pending events", key, n)
		}
	}
	t.Logf("backlog=2000, docsExamined=%d, keysExamined=%d, blockingSort=false", stats.DocsExamined, stats.KeysExamined)
}

func planHas(value any, key, want string) bool {
	switch v := value.(type) {
	case bson.D:
		for _, element := range v {
			if element.Key == key && element.Value == want {
				return true
			}
			if planHas(element.Value, key, want) {
				return true
			}
		}
	case bson.M:
		if v[key] == want {
			return true
		}
		for _, child := range v {
			if planHas(child, key, want) {
				return true
			}
		}
	case bson.A:
		for _, child := range v {
			if planHas(child, key, want) {
				return true
			}
		}
	}
	return false
}
