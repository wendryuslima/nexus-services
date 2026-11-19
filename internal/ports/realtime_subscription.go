package ports

import "context"

type RealtimeSubscription interface{ Close() error }

type RealtimeSubscriber interface {
	Healthy() bool
	Subscribe(ctx context.Context, userID string, onEvent func(RealtimeEvent), onFailure func()) (RealtimeSubscription, error)
}
