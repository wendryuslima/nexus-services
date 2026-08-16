package ports

import "context"

type IDGenerator interface {
	New(ctx context.Context) (string, error)
}
