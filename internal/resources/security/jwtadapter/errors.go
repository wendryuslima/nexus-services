package jwtadapter

import "errors"

var (
	ErrInvalidConfiguration = errors.New("invalid JWT configuration")
	ErrInvalidClaims        = errors.New("invalid JWT claims")
)
