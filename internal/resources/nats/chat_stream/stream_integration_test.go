package chatstream

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestEnsureIntegration(t *testing.T) {
	url := os.Getenv("NATS_PROVISION_TEST_URL")
	if url == "" {
		t.Skip("set NATS_PROVISION_TEST_URL to a dedicated empty JetStream broker")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := js.Stream(ctx, Name); !errors.Is(err, jetstream.ErrStreamNotFound) {
		t.Fatalf("test requires an absent %s stream; got %v", Name, err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := js.DeleteStream(cleanup, Name); err != nil {
			t.Error(err)
		}
	}()

	// Simulate workers starting together on a fresh installation.
	start := make(chan struct{})
	results := make(chan error, 8)
	for range cap(results) {
		go func() {
			<-start
			results <- Ensure(ctx, js)
		}()
	}
	close(start)
	for range cap(results) {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	stream, err := js.Stream(ctx, Name)
	if err != nil {
		t.Fatal(err)
	}
	info := stream.CachedInfo()
	if info.Config.Storage != jetstream.FileStorage || info.Config.Replicas != 1 ||
		info.Config.Retention != jetstream.LimitsPolicy || info.Config.Discard != jetstream.DiscardOld ||
		info.Config.MaxAge != 7*24*time.Hour || info.Config.MaxBytes != 1<<30 {
		t.Fatalf("unexpected initial stream configuration: %+v", info.Config)
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, Name, jetstream.ConsumerConfig{
		Durable: "MESSAGE_DELIVERY", FilterSubject: "chat.message.created.>",
		AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"chat.message.created.0", "chat.message.delivery_failed"} {
		if _, err := js.Publish(ctx, subject, []byte("test"), jetstream.WithExpectStream(Name)); err != nil {
			t.Fatal(err)
		}
	}

	// A restart must preserve operator settings and stored events/consumers.
	cfg := info.Config
	cfg.MaxAge = 3 * 24 * time.Hour
	cfg.MaxBytes = 2 << 30
	stream, err = js.UpdateStream(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	before := stream.CachedInfo()
	if err := Ensure(ctx, js); err != nil {
		t.Fatal(err)
	}
	after, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Config, after.Config) || after.State.Msgs != 2 || after.State.Consumers != 1 {
		t.Fatalf("existing stream changed: before=%+v after=%+v", before, after)
	}
	msg, err := consumer.Next(jetstream.FetchMaxWait(3 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject() != "chat.message.created.0" || string(msg.Data()) != "test" {
		t.Fatalf("unexpected delivery: subject=%s data=%s", msg.Subject(), msg.Data())
	}
	if err := msg.DoubleAck(ctx); err != nil {
		t.Fatal(err)
	}
}
