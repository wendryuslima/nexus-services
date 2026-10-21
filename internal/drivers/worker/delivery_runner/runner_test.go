package deliveryrunner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type messageStub struct {
	data       []byte
	metadata   *jetstream.MsgMetadata
	acked      int
	nacked     int
	termed     int
	nakDelay   time.Duration
	termReason string
	onAck      func()
	onTerm     func()
}

func (stub *messageStub) Metadata() (*jetstream.MsgMetadata, error) { return stub.metadata, nil }
func (stub *messageStub) Data() []byte                              { return stub.data }
func (stub *messageStub) Headers() nats.Header                      { return nil }
func (stub *messageStub) Subject() string                           { return "chat.message.created.0" }
func (stub *messageStub) Reply() string                             { return "" }
func (stub *messageStub) Ack() error                                { stub.acked++; return nil }
func (stub *messageStub) Nak() error                                { stub.nacked++; return nil }
func (stub *messageStub) InProgress() error                         { return nil }
func (stub *messageStub) Term() error                               { stub.termed++; return nil }
func (stub *messageStub) DoubleAck(context.Context) error {
	if stub.onAck != nil {
		stub.onAck()
	}
	stub.acked++
	return nil
}
func (stub *messageStub) NakWithDelay(delay time.Duration) error {
	stub.nacked++
	stub.nakDelay = delay
	return nil
}
func (stub *messageStub) TermWithReason(reason string) error {
	if stub.onTerm != nil {
		stub.onTerm()
	}
	stub.termed++
	stub.termReason = reason
	return nil
}

type handlerStub struct {
	err   error
	calls int
}

func (stub *handlerStub) Dispatch(context.Context, ports.MessageCreatedEvent) error {
	stub.calls++
	return stub.err
}

type failureStub struct {
	failure ports.DeliveryFailure
	err     error
	calls   int
}

func (stub *failureStub) PublishFailure(_ context.Context, failure ports.DeliveryFailure) error {
	stub.calls++
	stub.failure = failure
	return stub.err
}

func runnerFixture(t *testing.T, handler *handlerStub, failures *failureStub) (*Runner, *messageStub) {
	t.Helper()
	data, err := json.Marshal(ports.MessageCreatedEvent{EventID: "event-1"})
	if err != nil {
		t.Fatal(err)
	}
	msg := &messageStub{
		data: data,
		metadata: &jetstream.MsgMetadata{
			Stream:       "CHAT_EVENTS",
			Consumer:     "MESSAGE_DELIVERY",
			Sequence:     jetstream.SequencePair{Stream: 12},
			NumDelivered: 1,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Runner{handler: handler, failures: failures, logger: logger}, msg
}

func TestProcessMessageAcksOnlyAfterDispatch(t *testing.T) {
	handler := &handlerStub{}
	failures := &failureStub{}
	runner, msg := runnerFixture(t, handler, failures)
	msg.onAck = func() {
		if handler.calls != 1 {
			t.Fatal("JetStream ACK happened before dispatch")
		}
	}
	if err := runner.processMessage(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if msg.acked != 1 || msg.nacked != 0 || msg.termed != 0 {
		t.Fatalf("wrong disposition: ack=%d nak=%d term=%d", msg.acked, msg.nacked, msg.termed)
	}
}

func TestProcessMessageRetriesTransientFailure(t *testing.T) {
	handler := &handlerStub{err: errors.New("temporary NATS failure")}
	runner, msg := runnerFixture(t, handler, &failureStub{})
	if err := runner.processMessage(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if msg.acked != 0 || msg.nacked != 1 || msg.termed != 0 || msg.nakDelay != 5*time.Second {
		t.Fatalf("wrong transient disposition: ack=%d nak=%d term=%d delay=%s", msg.acked, msg.nacked, msg.termed, msg.nakDelay)
	}
}

func TestProcessMessageRecordsPermanentFailureBeforeTerm(t *testing.T) {
	handler := &handlerStub{err: ports.ErrInvalidMessageCreatedEvent}
	failures := &failureStub{}
	runner, msg := runnerFixture(t, handler, failures)
	msg.onTerm = func() {
		if failures.calls != 1 {
			t.Fatal("invalid event terminated before failure was recorded")
		}
	}
	if err := runner.processMessage(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if failures.calls != 1 || msg.termed != 1 || msg.acked != 0 || msg.termReason != "invalid_event" {
		t.Fatalf("wrong permanent disposition: failures=%d term=%d ack=%d", failures.calls, msg.termed, msg.acked)
	}
}

func TestProcessMessageDoesNotTermWhenFailureRecordFails(t *testing.T) {
	handler := &handlerStub{err: ports.ErrInvalidMessageCreatedEvent}
	failures := &failureStub{err: errors.New("JetStream unavailable")}
	runner, msg := runnerFixture(t, handler, failures)
	if err := runner.processMessage(context.Background(), msg); err == nil {
		t.Fatal("expected failure publication error")
	}
	if msg.termed != 0 || msg.nacked != 1 || msg.acked != 0 {
		t.Fatalf("invalid event was terminated without a failure record: term=%d nak=%d ack=%d", msg.termed, msg.nacked, msg.acked)
	}
}
