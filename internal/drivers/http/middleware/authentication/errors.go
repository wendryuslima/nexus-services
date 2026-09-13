package authentication

import "errors"

var (
	ErrNilAuthenticator     = errors.New("authenticator cannot be nil")
	ErrNilAccessTokenReader = errors.New("access token reader cannot be nil")
	ErrNilHandler           = errors.New("protected HTTP handler cannot be nil")
	ErrNilLogger            = errors.New("authentication logger cannot be nil")
)
