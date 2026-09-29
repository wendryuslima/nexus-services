package timeline

import "errors"

var (
	ErrInvalidID          = errors.New("timeline item id cannot be empty")
	ErrInvalidChatID      = errors.New("timeline item chat id cannot be empty")
	ErrInvalidKind        = errors.New("invalid timeline item kind")
	ErrInvalidCreatedAt   = errors.New("timeline item created at cannot be zero")
	ErrInvalidUpdatedAt   = errors.New("timeline item updated at is invalid")
	ErrInvalidSenderID    = errors.New("message sender id cannot be empty")
	ErrInvalidContentType = errors.New("invalid message content type")
	ErrInvalidContent     = errors.New("message content cannot be empty")
)
