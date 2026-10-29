package realtimesubscriber

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

func TestCoreNATSFanoutAcrossInstances(t *testing.T) {
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("set NATS_TEST_URL for real broker integration")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := New(url, 32768, time.Second, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close(ctx)
	second, err := New(url, 32768, time.Second, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(ctx)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !first.Healthy() || !second.Healthy() {
		select {
		case <-ctx.Done():
			t.Fatal("broker not ready")
		case <-ticker.C:
		}
	}
	a := make(chan ports.RealtimeMessageCreated, 1)
	b := make(chan ports.RealtimeMessageCreated, 1)
	failures := make(chan struct{}, 4)
	fail := func() {
		select {
		case failures <- struct{}{}:
		default:
		}
	}
	subscription, err := first.Subscribe(ctx, "user-1", func(event ports.RealtimeMessageCreated) { a <- event }, fail)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	subscription2, err := second.Subscribe(ctx, "user-1", func(event ports.RealtimeMessageCreated) { b <- event }, fail)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription2.Close()
	connection, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	event := ports.RealtimeMessageCreated{Version: 1, Type: "message.created", EventID: "event-1", OccurredAt: time.Now(), Data: ports.RealtimeMessageData{ID: "message-1", ChatID: "chat-1", Sequence: 1, SenderID: "sender-1", CreatedAt: time.Now(), Content: ports.OutboxContent{Type: "TEXT", Value: "Olá"}}}
	raw, _ := json.Marshal(event)
	if err := connection.Publish("realtime.user.user-1", raw); err != nil {
		t.Fatal(err)
	}
	if err := connection.FlushWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	for _, events := range []chan ports.RealtimeMessageCreated{a, b} {
		select {
		case received := <-events:
			if received.EventID != event.EventID {
				t.Fatal("event identity changed")
			}
		case <-ctx.Done():
			t.Fatal("one instance missed the event")
		}
	}
	if err := connection.Publish("realtime.user.user-1", []byte(`{"version":99}`)); err != nil {
		t.Fatal(err)
	}
	connection.FlushWithContext(ctx)
	select {
	case <-failures:
	case <-ctx.Done():
		t.Fatal("invalid event was not reported")
	}
	first.conn.Close()
	select {
	case <-failures:
	case <-ctx.Done():
		t.Fatal("disconnect was not reported")
	}
	if first.Healthy() {
		t.Fatal("closed connection remained healthy")
	}
}
