package messagestore

import "errors"

var (
	ErrNilTransactionRunner = errors.New(
		"message store transaction runner cannot be nil",
	)

	ErrNilChatsCollection = errors.New(
		"message store chats collection cannot be nil",
	)

	ErrNilTimelineCollection = errors.New(
		"message store timeline collection cannot be nil",
	)

	ErrNilOutboxCollection = errors.New(
		"message store outbox collection cannot be nil",
	)

	ErrInvalidCreateParams = errors.New(
		"invalid create message parameters",
	)
)
