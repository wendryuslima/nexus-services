package router

import "errors"

var (
	ErrNilAuthHandler     = errors.New("authentication HTTP handler cannot be nil")
	ErrNilBrowserSecurity = errors.New("broser security middleware cannot be nil")
	ErrNilLogger          = errors.New("router logger cannot be nil")
	ErrNilUserHandler     = errors.New("user HTTP handler cannot be nil")
	ErrNilAuthentication  = errors.New("authentication middleware cannot be nil")
)
