package ports

import "errors"

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailAlreadyExists = errors.New("email already exists")
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionConflict    = errors.New("session updated conflict")
	ErrInvalidToken       = errors.New("invalid token")
	ErrExpiredToken       = errors.New("token expired")
)
