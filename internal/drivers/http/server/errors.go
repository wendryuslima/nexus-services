package server

import "errors"

var (
	ErrNilHandler = errors.New("HTTP server handler cannot be nil")

	ErrNilLogger = errors.New("HTTP server logger cannot be nil")
)
