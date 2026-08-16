package user

import "errors"

var (
	ErrInvalidID           = errors.New("invalid user id")
	ErrInvalidEmail        = errors.New("invalid email")
	ErrInvalidPassword     = errors.New("invalid password")
	ErrInvalidPasswordHash = errors.New("invalid password hash")
	ErrInvalidCreatedAt    = errors.New("inavlid user creation date")
)
