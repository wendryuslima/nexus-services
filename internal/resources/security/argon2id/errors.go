package argon2id

import "errors"

var (
	ErrEmptyPassword     = errors.New("password cannot be empty")
	ErrInvalidParameters = errors.New("invalid Argon2id parameters")
	ErrInvalidEncodeHash = errors.New("invalid encoded Argon2id hash")
)
