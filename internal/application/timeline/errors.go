package timeline

import "errors"

var (
	ErrNilDependency        = errors.New("nil timeline use case dependency")
	ErrInvalidCurrentUserID = errors.New("invalid current user id")
	ErrInvalidChatID        = errors.New("invalid chat id")
	ErrInvalidPageSize      = errors.New("invalid timeline page size")
	ErrInvalidCursor        = errors.New("invalid timeline cursor")
	ErrChatNotFound         = errors.New("chat not found")
)
