package imagecleanup

import (
	"context"
	"log/slog"
	"time"
)

type Cleaner interface {
	CleanupOnce(context.Context) (bool, error)
}

func Run(ctx context.Context, cleaner Cleaner, logger *slog.Logger) {
	for ctx.Err() == nil {
		for range 100 {
			worked, err := cleaner.CleanupOnce(ctx)
			if err != nil {
				if ctx.Err() == nil {
					logger.Error("image cleanup failed", slog.Any("error", err))
				}
				break
			}
			if !worked {
				break
			}
		}
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
