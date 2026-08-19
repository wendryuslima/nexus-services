package auth

import "errors"

var (
	ErrNilDependency         = errors.New("nil use case dependdency")
	ErrEmailAlreadyRegistred = errors.New("email already registred")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrInvalidRefreshToken   = errors.New("invalid refresh token")
	ErrInvalidConfiguration  = errors.New("invalid use case configuration")
)
