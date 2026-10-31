package image

import "time"

const (
	Pending             = "PENDING"
	Ready               = "READY"
	Attached            = "ATTACHED"
	Deleting            = "DELETING"
	Deleted             = "DELETED"
	MaxBytes      int64 = 10 << 20
	MaxDimension        = 8192
	MaxPixels           = 16_000_000
	UploadLease         = 2 * time.Minute
	AbandonAfter        = 24 * time.Hour
	DeletionGrace       = 5 * time.Minute
)

type Asset struct {
	ID, UploadedBy, ChatID, ClientUploadID      string
	Bucket, ObjectKey, ContentType, Checksum    string
	SizeBytes                                   int64
	Width, Height                               int
	Status, MessageID, UploadToken, DeleteToken string
	CreatedAt, ExpiresAt, DeleteStartedAt       time.Time
}
