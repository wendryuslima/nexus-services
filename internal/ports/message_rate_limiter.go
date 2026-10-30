package ports

import (
	"context"
	"time"
)

type MessageRateLimiter interface {
	Reserve(ctx context.Context, userID string) (time.Duration, error)
}
