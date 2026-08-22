package userrepository

import "errors"

var (
	ErrNilCollection   = errors.New("MongoDB collection cannote be nil")
	ErrNilUser         = errors.New("user cannot be nil")
	ErrInvalidDocument = errors.New("invalid MongoDB user document")
)
