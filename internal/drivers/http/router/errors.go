package router

import "errors"

var (
	ErrNilAuthHandler     = errors.New("authentication HTTP handler cannot be nil")
	ErrNilBrowserSecurity = errors.New("broser security middleware cannot be nil")
	ErrNilLogger          = errors.New("router logger cannot be nil")
)
