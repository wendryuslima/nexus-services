package chat

import "errors"

var (
	ErrInvalidID            = errors.New("chat id cannot be empty")
	ErrInvalidParticipant   = errors.New("chat participant cannot be empty")
	ErrSameParticipant      = errors.New("direct chat participants must be different")
	ErrInvalidCreatedAt     = errors.New("chat created at cannot be zero")
	ErrInvalidUpdatedAt     = errors.New("chat updated at is invalid")
	ErrInvalidSummarySortAt = errors.New("chat summary sort at is invalid")
	ErrUserNotParticipant   = errors.New("user is not a chat participant")
)
