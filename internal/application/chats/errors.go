package chats

import "errors"

var (
	ErrNilDependency          = errors.New("nil chat use case dependency")
	ErrInvalidCurrentUserID   = errors.New("invalid current user id")
	ErrInvalidRelatedUserID   = errors.New("invalid related user id")
	ErrSelfDirectChat         = errors.New("cannot create a direct chat with yourself")
	ErrRelatedUserNotFound    = errors.New("related user not found")
	ErrInvalidPage            = errors.New("invalid chat list page")
	ErrInvalidPageSize        = errors.New("invalid chat list page size")
	ErrInvalidCursor          = errors.New("invalid chat list cursor")
	ErrInconsistentPagination = errors.New("chat list page and cursor are inconsistent")
)
