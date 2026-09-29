package timelinerepository

import "errors"

var (
	ErrNilCollection     = errors.New("MongoDB timeline collection cannot be nil")
	ErrInvalidDocument   = errors.New("invalid MongoDB timeline document")
	ErrInvalidListChat   = errors.New("timeline list chat cannot be empty")
	ErrInvalidListLimit  = errors.New("timeline list limit must be greater than zero")
	ErrInvalidListCursor = errors.New("timeline list cursor is invalid")
)
