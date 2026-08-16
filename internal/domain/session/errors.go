package session

import "errors"

var (
	ErrInvalidID            = errors.New("invalid session id")
	ErrInvalidUserID        = errors.New("invalid session user id")
	ErrInvalidTokenDigest   = errors.New("invalid refresh token digest")
	ErrInvalidPeriod        = errors.New("invalid session period")
	ErrExpired              = errors.New("session expired")
	ErrRevoked              = errors.New("session revoked")
	ErrRefreshTokenMismatch = errors.New("refresh token does not match session")
)
