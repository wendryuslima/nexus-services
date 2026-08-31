package recovery

import "errors"

var (
	ErrNilHandler = errors.New("recovery middleware handler cannot be nil")
	ErrNilLogger  = errors.New("recovery middleware logger cannot be nil")
)
