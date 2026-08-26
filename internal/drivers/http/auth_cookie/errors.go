package authcookie

import "errors"

var (
	ErrInvalidConfig = errors.New("invalid authentication cookie configuration")

	ErrNilDependency = errors.New("nil authentication cookie dependency")

	ErrEmptyToken = errors.New("authentication token cannot be empty")

	ErrInvalidExpiration = errors.New("invalid authentication cookie expiration")

	ErrCookieNotFound = errors.New("authentication cookie not found")

	ErrDuplicateCookie = errors.New("duplicate authentication cookie")
)
