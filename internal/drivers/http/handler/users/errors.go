package users

import "errors"

var (
	ErrNilUseCase = errors.New("use case cannot be nil")
	ErrNilLogger  = errors.New("logger cannot be nil")
)
