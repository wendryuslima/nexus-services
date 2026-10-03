package outboxrepository

import "errors"

var (
	ErrNilCollection = errors.New("outbox collection cannot be nil")
)
