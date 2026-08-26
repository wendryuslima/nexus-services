package response

import "errors"

var (
	ErrNilWriter = errors.New("HTTP response writer cannot be nil")

	ErrInvalidStatus = errors.New("invalid HTTP status")

	ErrInvalidErrorResponse = errors.New("invalid HTTP error response")
)
