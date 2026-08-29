package auth

import "errors"

var (
	ErrNilUseCase       = errors.New("HTTP handler use case cannot be nil")
	ErrNilLogger        = errors.New("HTTP handler logger cannot be nil")
	ErrNilCookieManager = errors.New("authentication cookie manager cannot be nil")
)
