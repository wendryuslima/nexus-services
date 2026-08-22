package config

import "errors"

var (
	ErrMissingEnvironmentVariable = errors.New("missing required environment variable")
	ErrInvalidEnvironmentVariable = errors.New("invalid environment variable")
	ErrInvalidMongoDBConfig       = errors.New("invalid MongoDB configuratio")
)
