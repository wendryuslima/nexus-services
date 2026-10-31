package outboxrunner

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	applicationoutbox "github.com/wendryuslima/nexus-services/internal/application/outbox"
	"github.com/wendryuslima/nexus-services/internal/resources/clock"
)

type stepFunc func(context.Context) (applicationoutbox.Outcome, error)

func (f stepFunc) RunOnce(ctx context.Context) (applicationoutbox.Outcome, error) { return f(ctx) }

type recoveryFunc func(context.Context, time.Time, int) (int64, error)

func (f recoveryFunc) RecoverExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	return f(ctx, now, limit)
}

func TestRunnerBoundsConcurrencyRecoversWhileBusyAndDrainsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const concurrency = 4
	entered := make(chan struct{}, concurrency+1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	recovered := make(chan int, 16)
	var active, maximum atomic.Int32
	step := stepFunc(func(context.Context) (applicationoutbox.Outcome, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); current > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-time.After(4 * time.Second):
		}
		return applicationoutbox.OutcomePublished, nil
	})
	recoverer := recoveryFunc(func(ctx context.Context, now time.Time, limit int) (int64, error) {
		if now.IsZero() {
			t.Error("recovery did not receive current time")
		}
		select {
		case recovered <- limit:
		case <-ctx.Done():
		}
		return 0, nil
	})
	runner, err := NewRunner(step, recoverer, clock.NewSystemClock(), slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		Concurrency: concurrency, IdleInterval: time.Second, ErrorInterval: time.Second,
		RecoveryInterval: 5 * time.Millisecond, RecoveryBatchSize: 17,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	for range concurrency {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("workers did not run concurrently")
		}
	}
	for range 2 {
		select {
		case limit := <-recovered:
			if limit != 17 {
				t.Fatalf("recovery limit = %d", limit)
			}
		case <-ctx.Done():
			t.Fatal("recovery was blocked by publications")
		}
	}
	cancel()
	select {
	case <-done:
		t.Fatal("shutdown abandoned in-flight publications")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("workers did not shut down")
	}
	if maximum.Load() != concurrency || active.Load() != 0 {
		t.Fatalf("maximum=%d active=%d", maximum.Load(), active.Load())
	}
}
