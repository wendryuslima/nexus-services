package mongodb

import "errors"

var (
	ErrInvalidCollectionName   = errors.New("invalid MongoDB collection name")
	ErrUninitializedClient     = errors.New("MongoDB client is not initialized")
	ErrNilTransactionOperation = errors.New("MongoDB transaction operation cannot be nil")
)
