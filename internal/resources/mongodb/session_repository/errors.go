package sessionrepository

import "errors"

var (
	ErrNilCollection       = errors.New("MongoDB collection cannot be nil")
	ErrNilSession          = errors.New("session cannot be nil")
	ErrInvalidDocument     = errors.New("invalid MongoDB session document")
	ErrInvalidExpectDigest = errors.New("expected refresh token digest cannot be empty")
	ErrInvalidRotationTime = errors.New("rotation time cannot be empty")
)
