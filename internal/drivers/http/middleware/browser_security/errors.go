package browsersecurity

import "errors"

var (
	ErrNilHandler             = errors.New("protected HTTP handler cannot be nil")
	ErrNilLogger              = errors.New("browser security logger cannot be nil")
	ErrMissingAllowedOrigins  = errors.New("allowed origins cannot be empty")
	ErrInvalidOrigin          = errors.New("invalid allowed origin")
	ErrInvalidPreflightMaxAge = errors.New("invalid CORS preflight max age")
)
