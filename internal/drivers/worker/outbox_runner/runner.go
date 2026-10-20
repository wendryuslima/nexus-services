package outboxrunner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	applicationoutbox "github.com/wendryuslima/nexus-services/internal/application/outbox"
)

type Step interface {
	RunOnce(ctx context.Context) (applicationoutbox.Outcome, error)
}

type Runner struct {
	step          Step
	logger        *slog.Logger
	idleInterval  time.Duration
	errorInterval time.Duration
}

func NewRunner(
	step Step,
	logger *slog.Logger,
	idleInterval time.Duration,
	errorInterval time.Duration,
) (*Runner, error) {
	if step == nil {
		return nil, errors.New("outbox step cannot be nil")
	}
	if logger == nil {
		return nil, errors.New("outbox logger cannot be nil")
	}
	if idleInterval <= 0 || errorInterval <= 0 {
		return nil, errors.New("outbox intervals must be positive")
	}

	return &Runner{
		step:          step,
		logger:        logger,
		idleInterval:  idleInterval,
		errorInterval: errorInterval,
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

	for {

		if ctx.Err() != nil {
			return nil
		}

		outcome, err := runner.step.RunOnce(ctx)
		if err != nil {

			if ctx.Err() != nil {
				return nil
			}

			runner.logger.Error(
				"outbox step could not finish",
				slog.Any("error", err),
			)
			if !wait(ctx, runner.errorInterval) {
				return nil
			}
			continue
		}

		switch outcome {
		case applicationoutbox.OutcomeNoWork:

			if !wait(ctx, runner.idleInterval) {
				return nil
			}

		case applicationoutbox.OutcomePublished:

		case applicationoutbox.OutcomeRetryScheduled:

			runner.logger.Warn("outbox publication retry scheduled")
			if !wait(ctx, runner.errorInterval) {
				return nil
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
			if !wait(ctx, runner.errorInterval) {
				return nil
			}
		}
	}
}
