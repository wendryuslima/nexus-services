package ports

import (
	"context"
	"errors"
	"io"
	"time"

	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
)

var (
	ErrImageNotFound       = errors.New("image not found")
	ErrImageUnavailable    = errors.New("image is not available for attachment")
	ErrImageUploadConflict = errors.New("image upload idempotency conflict")
	ErrImageUploadBusy     = errors.New("image upload is in progress")
	ErrImageExpired        = errors.New("image upload expired")
)

type ImageRepository interface {
	ReserveUpload(ctx context.Context, candidate domainimage.Asset, now time.Time) (*domainimage.Asset, bool, error)
	MarkReady(ctx context.Context, id, uploadToken string, now time.Time) (bool, error)
	ReleaseUpload(ctx context.Context, id, uploadToken string) error
	FindByID(ctx context.Context, id string) (*domainimage.Asset, error)
	ClaimAbandoned(ctx context.Context, now time.Time, deleteToken string) (*domainimage.Asset, error)
	CompleteDeletion(ctx context.Context, asset domainimage.Asset, now time.Time) error
}

type ImageStorage interface {
	Put(ctx context.Context, asset domainimage.Asset, body io.ReadSeeker) error
	Delete(ctx context.Context, bucket, key string) error
	PresignGet(ctx context.Context, asset domainimage.Asset, ttl time.Duration) (string, error)
}
