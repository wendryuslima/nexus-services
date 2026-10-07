package users

import "errors"

var ErrNilDependency = errors.New("nil use case dependency")
var ErrInvalidPage = errors.New("invalid user list page")
var ErrInvalidPageSize = errors.New("invalid user list page size")
var ErrInvalidCursor = errors.New("invalid user list cursor")
var ErrInconsistentPagination = errors.New("user list page and cursor are inconsistent")
