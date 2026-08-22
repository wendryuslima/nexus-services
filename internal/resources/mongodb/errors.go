package mongodb

import "errors"

var (
	ErrInvalidCollectionName = errors.New("invalid MongoDB collection name")
)
