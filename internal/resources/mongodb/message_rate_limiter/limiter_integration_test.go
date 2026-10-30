package messageratelimiter

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func newIntegrationLimiter(t *testing.T) (*Limiter, *mongo.Collection) {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI to run MongoDB integration tests")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetTimeout(10 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	database := client.Database(fmt.Sprintf("nexus_rate_limit_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := database.Drop(ctx); err != nil {
			t.Error(err)
		}
		if err := client.Disconnect(ctx); err != nil {
			t.Error(err)
		}
	})
	collection := database.Collection("message_rate_limits")
	limiter, err := New(collection)
	if err != nil {
		t.Fatal(err)
	}
	if err := limiter.EnsureIndexes(context.Background()); err != nil {
		t.Fatal(err)
	}
	return limiter, collection
}

func TestReserveSlidingWindows(t *testing.T) {
	limiter, collection := newIntegrationLimiter(t)
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	repeated := func(count int, age time.Duration) []time.Time {
		values := make([]time.Time, count)
		for i := range values {
			values[i] = now.Add(-age)
		}
		return values
	}
	for _, test := range []struct {
		name       string
		timestamps []time.Time
		wantWait   time.Duration
		wantCount  int
	}{
		{"first attempt", nil, 0, 1},
		{"thirtieth allowed", repeated(29, 0), 0, 30},
		{"thirty first rejected", repeated(30, 0), 10 * time.Second, 30},
		{"burst expires at ten seconds", repeated(30, 10*time.Second), 0, 31},
		{"sixtieth allowed", append(repeated(30, 40*time.Second), repeated(29, 20*time.Second)...), 0, 60},
		{"sixty first rejected", append(repeated(30, 40*time.Second), repeated(30, 20*time.Second)...), 20 * time.Second, 60},
		{"longest wait wins", append(repeated(30, 15*time.Second), repeated(30, 5*time.Second)...), 45 * time.Second, 60},
		{"minute expires at sixty seconds", append(repeated(30, time.Minute), repeated(30, 40*time.Second)...), 0, 31},
		{"stale TTL document", repeated(60, 2*time.Minute), 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.timestamps == nil {
				test.timestamps = []time.Time{}
			}
			_, err := collection.InsertOne(ctx, bson.M{"_id": test.name, "checked_at": now, "timestamps": test.timestamps})
			if err != nil {
				t.Fatal(err)
			}
			wait, err := limiter.Reserve(ctx, test.name)
			if err != nil || wait != test.wantWait {
				t.Fatalf("wait=%s err=%v, want %s", wait, err, test.wantWait)
			}
			var stored struct {
				Timestamps []time.Time `bson:"timestamps"`
				ExpiresAt  time.Time   `bson:"expires_at"`
			}
			if err := collection.FindOne(ctx, bson.M{"_id": test.name}).Decode(&stored); err != nil {
				t.Fatal(err)
			}
			if len(stored.Timestamps) != test.wantCount {
				t.Fatalf("stored %d attempts, want %d", len(stored.Timestamps), test.wantCount)
			}
			if test.wantWait > 0 {
				if !stored.ExpiresAt.Equal(test.timestamps[len(test.timestamps)-1].Add(time.Minute)) {
					t.Fatalf("blocked attempt extended expiry: %s", stored.ExpiresAt)
				}
				waitAgain, err := limiter.Reserve(ctx, test.name)
				if err != nil || waitAgain != wait {
					t.Fatalf("blocked retry changed wait: %s %v", waitAgain, err)
				}
				_, err = collection.UpdateOne(ctx, bson.M{"_id": test.name}, bson.M{"$set": bson.M{"checked_at": now.Add(wait)}})
				if err != nil {
					t.Fatal(err)
				}
				wait, err = limiter.Reserve(ctx, test.name)
				if err != nil || wait != 0 {
					t.Fatalf("not released at deadline: %s %v", wait, err)
				}
			}
		})
	}
}

func TestReserveConcurrentInstancesAndIndependentUsers(t *testing.T) {
	first, collection := newIntegrationLimiter(t)
	second, err := New(collection)
	if err != nil {
		t.Fatal(err)
	}
	var allowed, rejected atomic.Int64
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range 100 {
		group.Go(func() {
			<-start
			limiter := first
			if i%2 == 0 {
				limiter = second
			}
			wait, err := limiter.Reserve(context.Background(), "shared-user")
			if err != nil {
				t.Error(err)
				return
			}
			if wait == 0 {
				allowed.Add(1)
			} else {
				rejected.Add(1)
			}
		})
	}
	close(start)
	group.Wait()
	if allowed.Load() != 30 || rejected.Load() != 70 {
		t.Fatalf("expected 30 accepted and 70 rejected across instances, got %d/%d", allowed.Load(), rejected.Load())
	}
	if wait, err := second.Reserve(context.Background(), "other-user"); err != nil || wait != 0 {
		t.Fatalf("another user was blocked: %s %v", wait, err)
	}
	var stored struct {
		Timestamps []time.Time `bson:"timestamps"`
	}
	if err := collection.FindOne(context.Background(), bson.M{"_id": "shared-user"}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Timestamps) != 30 {
		t.Fatalf("rejected attempts consumed quota: %d", len(stored.Timestamps))
	}
}
