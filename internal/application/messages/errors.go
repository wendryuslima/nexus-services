package messages

import "errors"

var (
	ErrNilDependency          = errors.New("nil create message dependency")
	ErrInvalidCurrentUserID   = errors.New("invalid current user id")
	ErrInvalidChatID          = errors.New("invalid chat id")
	ErrInvalidClientMessageID = errors.New("invalid client message id")
	ErrInvalidContentType     = errors.New("invalid message content type")
	ErrInvalidContent         = errors.New("invalid message content")
	ErrContentTooLong         = errors.New("message content is too long")
	ErrChatNotFound           = errors.New("chat not found")
	ErrIdempotencyConflict    = errors.New("message idempotency conflict")
)
