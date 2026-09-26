package chatrepository

import "errors"

var (
	ErrNilCollection          = errors.New("MongoDB chat collection cannot be nil")
	ErrNilChat                = errors.New("chat cannot be nil")
	ErrInvalidDocument        = errors.New("invalid MongoDB chat document")
	ErrInvalidListParticipant = errors.New("chat list participant cannot be empty")
	ErrInvalidListLimit       = errors.New("chat list limit must be greater than zero")
	ErrInvalidListCursor      = errors.New("chat list cursor is invalid")
)
