package outbox

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
)

type Outcome string

const (
	OutcomeNoWork          Outcome = "NO_WORK"
	OutcomePublished       Outcome = "PUBLISHED"
	OutcomeRetryScheduled  Outcome = "RETRY_SCHEDULED"
	OutcomeFailedPermanent Outcome = "FAILED_PERMANENT"
)

type PublishOneDependencies struct {
	Store          ports.OutboxStore
	Publisher      ports.EventPublisher
	IDGenerator    ports.IDGenerator
	Clock          ports.Clock
	WorkerID       string
	LeaseDuration  time.Duration
	PublishTimeout time.Duration
	FinishTimeout  time.Duration
}

type PublishOne struct {
	store          ports.OutboxStore
	publisher      ports.EventPublisher
	idGenerator    ports.IDGenerator
	clock          ports.Clock
	workerID       string
	leaseDuration  time.Duration
	publishTimeout time.Duration
	finishTimeout  time.Duration
}

func NewPublishOne(deps PublishOneDependencies) (*PublishOne, error) {
	if deps.Store == nil ||
		deps.Publisher == nil ||
		deps.IDGenerator == nil ||
		deps.Clock == nil {
		return nil, errors.New("missing outbox publisher dependency")
	}
	if strings.TrimSpace(deps.WorkerID) == "" {
		return nil, errors.New("worker ID cannot be empty")
	}
	if deps.PublishTimeout <= 0 ||
		deps.FinishTimeout <= 0 ||
		deps.LeaseDuration <= deps.PublishTimeout+deps.FinishTimeout {
		return nil, errors.New("invalid outbox publisher timeouts")
	}

	return &PublishOne{
		store:          deps.Store,
		publisher:      deps.Publisher,
		idGenerator:    deps.IDGenerator,
		clock:          deps.Clock,
		workerID:       deps.WorkerID,
		leaseDuration:  deps.LeaseDuration,
		publishTimeout: deps.PublishTimeout,
		finishTimeout:  deps.FinishTimeout,
	}, nil
}

func retryDelay(attempts int64) time.Duration {
	delays := [...]time.Duration{
		time.Second,
		2 * time.Second,
		5 * time.Second,
		10 * time.Second,
		30 * time.Second,
		time.Minute,
		5 * time.Minute,
	}

	if attempts < 0 {
		attempts = 0
	}
	if attempts >= int64(len(delays)) {
		attempts = int64(len(delays) - 1)
	}

	base := delays[attempts]

	jitter := time.Duration(rand.Int64N(int64(base/5) + 1))
	return base + jitter
}

func (worker *PublishOne) RunOnce(ctx context.Context) (Outcome, error) {
	if worker == nil {
		return "", errors.New("outbox publisher worker is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	leaseID, err := worker.idGenerator.New(ctx)
	if err != nil {
		return "", fmt.Errorf("generate outbox lease ID: %w", err)
	}

	event, err := worker.store.ClaimNext(
		ctx,
		worker.workerID,
		leaseID,
		worker.clock.Now(),
		worker.leaseDuration,
	)
	if err != nil {
		return "", fmt.Errorf("claim outbox event: %w", err)
	}
	if event == nil {
		return OutcomeNoWork, nil
	}
	if event.LeaseID != leaseID {
		return "", errors.New("outbox store returned another lease ID")
	}

	publishCtx, cancelPublish := context.WithTimeout(
		ctx,
		worker.publishTimeout,
	)
	receipt, publishErr := worker.publisher.Publish(publishCtx, *event)
	cancelPublish()

	finishCtx, cancelFinish := context.WithTimeout(
		context.WithoutCancel(ctx),
		worker.finishTimeout,
	)
	defer cancelFinish()

	if publishErr == nil {

		updated, err := worker.store.MarkPublished(
			finishCtx,
			event.ID,
			leaseID,
			receipt,
			worker.clock.Now(),
		)
		if err != nil {
			return "", fmt.Errorf("finish published outbox event: %w", err)
		}
		if !updated {
			return "", fmt.Errorf("lease lost for outbox event %s", event.ID)
		}
		return OutcomePublished, nil
	}

	if errors.Is(publishErr, ports.ErrInvalidOutboxEvent) {

		updated, err := worker.store.MarkFailed(
			finishCtx,
			event.ID,
			leaseID,
			worker.clock.Now(),
			"invalid_outbox_event",
		)
		if err != nil {
			return "", fmt.Errorf("finish invalid outbox event: %w", err)
		}
		if !updated {
			return "", fmt.Errorf("lease lost for outbox event %s", event.ID)
		}
		return OutcomeFailedPermanent, nil
	}

	nextAttemptAt := worker.clock.Now().Add(retryDelay(event.Attempts))
	updated, err := worker.store.ScheduleRetry(
		finishCtx,
		event.ID,
		leaseID,
		nextAttemptAt,
		"outbox_publish_failed",
	)
	if err != nil {
		return "", fmt.Errorf("schedule outbox retry: %w", err)
	}
	if !updated {
		return "", fmt.Errorf("lease lost for outbox event %s", event.ID)
	}

	return OutcomeRetryScheduled, nil
}
