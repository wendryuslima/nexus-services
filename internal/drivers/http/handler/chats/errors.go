package chats

import "errors"

var (
	ErrNilUseCase            = errors.New("chat HTTP handler use case cannot be nil")
	ErrNilLogger             = errors.New("chat HTTP handler logger cannot be nil")
	ErrNilRateLimiter        = errors.New("chat HTTP handler rate limiter cannot be nil")
	ErrInvalidQueryParameter = errors.New("invalid chat query parameter")
)
