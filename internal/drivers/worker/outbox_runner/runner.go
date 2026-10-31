package outboxrunner

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	applicationoutbox "github.com/wendryuslima/nexus-services/internal/application/outbox"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type Step interface {
	RunOnce(ctx context.Context) (applicationoutbox.Outcome, error)
}

type Runner struct {
	step      Step
	logger    *slog.Logger
	recoverer ports.OutboxLeaseRecoverer
	clock     ports.Clock
	config    Config
}

type Config struct {
	Concurrency       int
	IdleInterval      time.Duration
	ErrorInterval     time.Duration
	RecoveryInterval  time.Duration
	RecoveryBatchSize int
}

func NewRunner(
	step Step,
	recoverer ports.OutboxLeaseRecoverer,
	clock ports.Clock,
	logger *slog.Logger,
	config Config,
) (*Runner, error) {
	if step == nil {
		return nil, errors.New("outbox step cannot be nil")
	}
	if logger == nil {
		return nil, errors.New("outbox logger cannot be nil")
	}
	if config.IdleInterval <= 0 || config.ErrorInterval <= 0 || config.RecoveryInterval <= 0 {
		return nil, errors.New("outbox intervals must be positive")
	}
	if recoverer == nil || clock == nil {
		return nil, errors.New("outbox recovery dependencies cannot be nil")
	}
	if config.Concurrency < 1 || config.Concurrency > 64 || config.RecoveryBatchSize < 1 || config.RecoveryBatchSize > 1000 {
		return nil, errors.New("invalid outbox concurrency or recovery batch size")
	}

	return &Runner{
		step:      step,
		logger:    logger,
		recoverer: recoverer,
		clock:     clock,
		config:    config,
	}, nil
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

func (runner *Runner) Run(ctx context.Context) error {
	if runner == nil {
		return errors.New("outbox runner is nil")
	}
	var workers sync.WaitGroup
	workers.Go(func() { runner.recoverLeases(ctx) })
	for range runner.config.Concurrency {
		workers.Go(func() { runner.publish(ctx) })
	}
	workers.Wait()
	return nil
}

func (runner *Runner) recoverLeases(ctx context.Context) {
	for ctx.Err() == nil {
		recovered, err := runner.recoverer.RecoverExpired(ctx, runner.clock.Now(), runner.config.RecoveryBatchSize)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			runner.logger.Error("outbox lease recovery failed", slog.Any("error", err))
		} else if recovered > 0 {
			runner.logger.Info("outbox leases recovered", slog.Int64("events", recovered))
		}
		if !wait(ctx, runner.config.RecoveryInterval) {
			return
		}
	}
}

func (runner *Runner) publish(ctx context.Context) {

	for {

		if ctx.Err() != nil {
			return
		}

		outcome, err := runner.step.RunOnce(ctx)
		if err != nil {

			if ctx.Err() != nil {
				return
			}

			runner.logger.Error(
				"outbox step could not finish",
				slog.Any("error", err),
			)
			if !wait(ctx, runner.config.ErrorInterval) {
				return
			}
			continue
		}

		switch outcome {
		case applicationoutbox.OutcomeNoWork:

			if !wait(ctx, runner.config.IdleInterval) {
				return
			}

		case applicationoutbox.OutcomePublished:

		case applicationoutbox.OutcomeRetryScheduled:

			runner.logger.Warn("outbox publication retry scheduled")
			if !wait(ctx, runner.config.ErrorInterval) {
				return
			}

		case applicationoutbox.OutcomeFailedPermanent:

			runner.logger.Error(
				"outbox event requires investigation",
			)

		default:

			runner.logger.Error(
				"outbox step returned an unknown outcome",
				slog.String("outcome", string(outcome)),
			)
			if !wait(ctx, runner.config.ErrorInterval) {
				return
			}
		}
	}
}
