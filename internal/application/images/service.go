package images

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

var ErrInvalidUpload = errors.New("invalid image upload")

type Service struct {
	images  ports.ImageRepository
	storage ports.ImageStorage
	chats   ports.ChatRepository
	ids     ports.IDGenerator
	clock   ports.Clock
	bucket  string
}

func New(images ports.ImageRepository, storage ports.ImageStorage, chats ports.ChatRepository, ids ports.IDGenerator, clock ports.Clock, bucket string) (*Service, error) {
	if images == nil || storage == nil || chats == nil || ids == nil || clock == nil || bucket == "" {
		return nil, errors.New("missing image dependency")
	}
	return &Service{images: images, storage: storage, chats: chats, ids: ids, clock: clock, bucket: bucket}, nil
}

type UploadInput struct {
	UserID, ChatID, ClientUploadID, ContentType, Checksum string
	SizeBytes                                             int64
	Width, Height                                         int
	Body                                                  io.ReadSeeker
}

type Metadata struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

func metadata(asset *domainimage.Asset) Metadata {
	return Metadata{ID: asset.ID, ContentType: asset.ContentType, SizeBytes: asset.SizeBytes, Width: asset.Width, Height: asset.Height}
}

func (s *Service) authorize(ctx context.Context, userID, chatID string) error {
	u, err := user.ParseID(userID)
	if err != nil {
		return ports.ErrChatNotFound
	}
	c, err := chat.ParseID(chatID)
	if err != nil {
		return ports.ErrChatNotFound
	}
	conversation, err := s.chats.FindByID(ctx, c)
	if err != nil {
		return err
	}
	if conversation == nil || !conversation.HasParticipant(u) {
		return ports.ErrChatNotFound
	}
	return nil
}

func (s *Service) Upload(ctx context.Context, input UploadInput) (Metadata, error) {
	clientUploadID, err := timeline.ParseClientMessageID(input.ClientUploadID)
	if err != nil {
		return Metadata{}, ErrInvalidUpload
	}
	input.ClientUploadID = clientUploadID.String()
	chatID, err := chat.ParseID(input.ChatID)
	if err != nil {
		return Metadata{}, ports.ErrChatNotFound
	}
	input.ChatID = chatID.String()
	if input.Body == nil || input.SizeBytes <= 0 || input.SizeBytes > domainimage.MaxBytes || input.Width < 1 || input.Height < 1 || input.Width > domainimage.MaxDimension || input.Height > domainimage.MaxDimension || int64(input.Width)*int64(input.Height) > domainimage.MaxPixels || len(input.Checksum) != 64 || (input.ContentType != "image/jpeg" && input.ContentType != "image/png") {
		return Metadata{}, ErrInvalidUpload
	}
	if err := s.authorize(ctx, input.UserID, input.ChatID); err != nil {
		return Metadata{}, err
	}
	id, err := s.ids.New(ctx)
	if err != nil {
		return Metadata{}, err
	}
	token, err := s.ids.New(ctx)
	if err != nil {
		return Metadata{}, err
	}
	now := s.clock.Now().UTC()
	candidate := domainimage.Asset{ID: id, UploadedBy: input.UserID, ChatID: input.ChatID, ClientUploadID: input.ClientUploadID, Bucket: s.bucket, ObjectKey: "images/" + id, ContentType: input.ContentType, Checksum: input.Checksum, SizeBytes: input.SizeBytes, Width: input.Width, Height: input.Height, Status: domainimage.Pending, UploadToken: token, CreatedAt: now, ExpiresAt: now.Add(domainimage.AbandonAfter)}
	asset, acquired, err := s.images.ReserveUpload(ctx, candidate, now)
	if err != nil {
		return Metadata{}, err
	}
	if !acquired {
		if asset.Status == domainimage.Ready || asset.Status == domainimage.Attached {
			return metadata(asset), nil
		}
		return Metadata{}, ports.ErrImageUploadBusy
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.images.ReleaseUpload(cleanup, asset.ID, asset.UploadToken)
	}()
	if _, err := input.Body.Seek(0, io.SeekStart); err != nil {
		return Metadata{}, err
	}
	if err := s.storage.Put(ctx, *asset, input.Body); err != nil {
		return Metadata{}, fmt.Errorf("store image: %w", err)
	}
	ready, err := s.images.MarkReady(ctx, asset.ID, asset.UploadToken, s.clock.Now().UTC())
	if err != nil {
		return Metadata{}, fmt.Errorf("confirm stored image: %w", err)
	}
	if !ready {
		return Metadata{}, ports.ErrImageExpired
	}
	return metadata(asset), nil
}

type Access struct {
	Metadata
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Service) Access(ctx context.Context, userID, id string) (Access, error) {
	asset, err := s.images.FindByID(ctx, id)
	if err != nil {
		return Access{}, err
	}
	if asset.Status != domainimage.Attached && !(asset.Status == domainimage.Ready && asset.UploadedBy == userID && asset.ExpiresAt.After(s.clock.Now())) {
		return Access{}, ports.ErrImageNotFound
	}
	if err := s.authorize(ctx, userID, asset.ChatID); err != nil {
		if errors.Is(err, ports.ErrChatNotFound) {
			return Access{}, ports.ErrImageNotFound
		}
		return Access{}, err
	}
	const ttl = 10 * time.Minute
	now := s.clock.Now().UTC()
	url, err := s.storage.PresignGet(ctx, *asset, ttl)
	if err != nil {
		return Access{}, err
	}
	return Access{Metadata: metadata(asset), URL: url, ExpiresAt: now.Add(ttl)}, nil
}

func (s *Service) CleanupOnce(ctx context.Context) (bool, error) {
	token, err := s.ids.New(ctx)
	if err != nil {
		return false, err
	}
	asset, err := s.images.ClaimAbandoned(ctx, s.clock.Now().UTC(), token)
	if err != nil || asset == nil {
		return false, err
	}
	if err := s.storage.Delete(ctx, asset.Bucket, asset.ObjectKey); err != nil {
		return true, err
	}
	return true, s.images.CompleteDeletion(ctx, *asset, s.clock.Now().UTC())
}
