package request

import "errors"

var (
	ErrNilRequest           = errors.New("http request cannot be nil")
	ErrNilDestination       = errors.New("JSON destination cannot be nil")
	ErrInvalidBodyLimit     = errors.New("invalid request body limit")
	ErrUnsupportedMediaType = errors.New("unsupported media type")
	ErrEmptyBody            = errors.New("request body cannot be empty")
	ErrBodyTooLarge         = errors.New("request body is too large")
	ErrMalformedJSON        = errors.New("malformed JSON body")
	ErrMultipleJSONValues   = errors.New("request body must contain a single JSON value")
)
