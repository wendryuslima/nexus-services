package tokendigest

import "errors"

var (
	ErrEmptyToken = errors.New("cannot digest an empty token")
)
