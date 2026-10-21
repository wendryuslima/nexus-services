package deliveryrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	defaultFetchWait       = 2 * time.Second
	defaultDispatchTimeout = 20 * time.Second
	defaultFinishTimeout   = 10 * time.Second
	defaultErrorWait       = 2 * time.Second
	maxEventBytes          = 64 * 1024
)

type Runner struct {
	consumer jetstream.Consumer
	handler  ports.MessageDeliveryHandler
	failures ports.DeliveryFailurePublisher
	logger   *slog.Logger
}

func NewRunner(consumer jetstream.Consumer, handler ports.MessageDeliveryHandler, failures ports.DeliveryFailurePublisher, logger *slog.Logger) (*Runner, error) {
	if consumer == nil || handler == nil || failures == nil || logger == nil {
		return nil, errors.New("missing delivery runner dependency")
	}
	return &Runner{consumer: consumer, handler: handler, failures: failures, logger: logger}, nil
}

func (runner *Runner) Run(ctx context.Context) error {
	if runner == nil {
		return errors.New("delivery runner is nil")
	}
	for ctx.Err() == nil {
		msg, err := runner.consumer.Next(jetstream.FetchMaxWait(defaultFetchWait))
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if errors.Is(err, nats.ErrTimeout) || errors.Is(err, jetstream.ErrNoMessages) {
				continue
			}
			runner.logger.Error("fetch delivery event failed", slog.Any("error", err))
			if !wait(ctx, defaultErrorWait) {
				break
			}
			continue
		}
		if err := runner.processMessage(ctx, msg); err != nil {
			runner.logger.Error("finish delivery event failed", slog.Any("error", err))
		}
	}
	return nil
}

func (runner *Runner) processMessage(ctx context.Context, msg jetstream.Msg) error {
	metadata, err := msg.Metadata()
	if err != nil {
		_ = msg.NakWithDelay(defaultErrorWait)
		return fmt.Errorf("read delivery metadata: %w", err)
	}
	if metadata.Stream != "CHAT_EVENTS" || metadata.Consumer != "MESSAGE_DELIVERY" {
		_ = msg.NakWithDelay(defaultErrorWait)
		return errors.New("delivery message came from an unexpected stream or consumer")
	}

	var event ports.MessageCreatedEvent
	if len(msg.Data()) > maxEventBytes || json.Unmarshal(msg.Data(), &event) != nil {
		return runner.failPermanent(ctx, msg, metadata, "", "invalid_envelope")
	}

	dispatchCtx, cancel := context.WithTimeout(ctx, defaultDispatchTimeout)
	dispatchErr := runner.handler.Dispatch(dispatchCtx, event)
	cancel()
	if errors.Is(dispatchErr, ports.ErrInvalidMessageCreatedEvent) {
		return runner.failPermanent(ctx, msg, metadata, event.EventID, "invalid_event")
	}
	if dispatchErr != nil {
		if err := msg.NakWithDelay(retryDelay(metadata.NumDelivered)); err != nil {
			return fmt.Errorf("request delayed delivery retry: %w", err)
		}
		runner.logger.Warn("delivery retry scheduled",
			slog.String("event_id", event.EventID),
			slog.Uint64("stream_sequence", metadata.Sequence.Stream),
			slog.Uint64("delivery_attempt", metadata.NumDelivered),
		)
		return nil
	}

	ackCtx, cancelAck := context.WithTimeout(context.WithoutCancel(ctx), defaultFinishTimeout)
	defer cancelAck()
	if err := msg.DoubleAck(ackCtx); err != nil {
		return fmt.Errorf("confirm delivery ack: %w", err)
	}
	runner.logger.Info("delivery event dispatched",
		slog.String("event_id", event.EventID),
		slog.Uint64("stream_sequence", metadata.Sequence.Stream),
	)
	return nil
}

func (runner *Runner) failPermanent(ctx context.Context, msg jetstream.Msg, metadata *jetstream.MsgMetadata, eventID, reasonCode string) error {
	failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultFinishTimeout)
	defer cancel()
	failure := ports.DeliveryFailure{
		EventID:        eventID,
		StreamSequence: metadata.Sequence.Stream,
		ReasonCode:     reasonCode,
	}
	if err := runner.failures.PublishFailure(failureCtx, failure); err != nil {
		_ = msg.NakWithDelay(retryDelay(metadata.NumDelivered))
		return fmt.Errorf("record permanent delivery failure: %w", err)
	}
	if err := msg.TermWithReason(reasonCode); err != nil {
		return fmt.Errorf("terminate invalid delivery event: %w", err)
	}
	runner.logger.Error("delivery event rejected permanently",
		slog.String("event_id", eventID),
		slog.Uint64("stream_sequence", metadata.Sequence.Stream),
		slog.String("reason_code", reasonCode),
	)
	return nil
}

func retryDelay(deliveries uint64) time.Duration {
	delays := [...]time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}
	if deliveries == 0 {
		return delays[0]
	}
	index := deliveries - 1
	if index >= uint64(len(delays)) {
		index = uint64(len(delays) - 1)
	}
	return delays[index]
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
