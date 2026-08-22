package sessionrepository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	sessionUserIDIndexName     = "sessions_user_id"
	sessionExpirationIndexName = "sessions_expiration_ttl"
)

var _ ports.SessionRepository = (*Repository)(nil)

type Repository struct {
	collection *mongo.Collection
}

func NewRepository(
	collection *mongo.Collection,
) (*Repository, error) {
	if collection == nil {
		return nil, ErrNilCollection
	}

	return &Repository{
		collection: collection,
	}, nil
}

func (repository *Repository) EnsureIndexes(
	ctx context.Context,
) error {
	indexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
			},
			Options: options.Index().
				SetName(sessionUserIDIndexName),
		},
		{
			Keys: bson.D{
				{Key: "expires_at", Value: 1},
			},
			Options: options.Index().
				SetName(sessionExpirationIndexName).
				SetExpireAfterSeconds(0),
		},
	}

	if _, err := repository.collection.
		Indexes().
		CreateMany(ctx, indexes); err != nil {
		return fmt.Errorf(
			"create session indexes: %w",
			err,
		)
	}

	return nil
}

func (repository *Repository) Create(
	ctx context.Context,
	authSession *session.Session,
) error {
	document, err := newSessionDocument(authSession)
	if err != nil {
		return err
	}

	if _, err := repository.collection.InsertOne(
		ctx,
		document,
	); err != nil {
		return fmt.Errorf(
			"insert session document: %w",
			err,
		)
	}

	return nil
}

func (repository *Repository) FindByID(
	ctx context.Context,
	id session.ID,
) (*session.Session, error) {
	if id.String() == "" {
		return nil, session.ErrInvalidID
	}

	var document sessionDocument

	err := repository.collection.FindOne(
		ctx,
		bson.D{
			{Key: "_id", Value: id.String()},
		},
	).Decode(&document)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ports.ErrSessionNotFound
		}

		return nil, fmt.Errorf(
			"find session document by id: %w",
			err,
		)
	}

	authSession, err := document.toDomain()
	if err != nil {
		return nil, fmt.Errorf(
			"convert session document to domain: %w",
			err,
		)
	}

	return authSession, nil
}

func (repository *Repository) Rotate(
	ctx context.Context,
	authSession *session.Session,
	expectedDigest string,
	rotatedAt time.Time,
) error {
	if authSession == nil {
		return ErrNilSession
	}

	if strings.TrimSpace(expectedDigest) == "" {
		return ErrInvalidExpectDigest
	}

	if rotatedAt.IsZero() {
		return ErrInvalidRotationTime
	}

	filter := bson.D{
		{
			Key:   "_id",
			Value: authSession.ID().String(),
		},
		{
			Key:   "refresh_token_digest",
			Value: expectedDigest,
		},
		{
			Key:   "revoked_at",
			Value: nil,
		},
		{
			Key: "expires_at",
			Value: bson.D{
				{
					Key:   "$gt",
					Value: rotatedAt.UTC(),
				},
			},
		},
	}

	update := bson.D{
		{
			Key: "$set",
			Value: bson.D{
				{
					Key:   "refresh_token_digest",
					Value: authSession.RefreshTokenDigest(),
				},
				{
					Key:   "expires_at",
					Value: authSession.ExpiresAt().UTC(),
				},
			},
		},
	}

	result, err := repository.collection.UpdateOne(
		ctx,
		filter,
		update,
	)
	if err != nil {
		return fmt.Errorf(
			"rotate session document: %w",
			err,
		)
	}

	if result.MatchedCount == 0 {
		return ports.ErrSessionConflict
	}

	return nil
}

func (repository *Repository) Revoke(
	ctx context.Context,
	id session.ID,
	revokedAt time.Time,
) error {
	if id.String() == "" {
		return session.ErrInvalidID
	}

	if revokedAt.IsZero() {
		return ErrInvalidRotationTime
	}

	filter := bson.D{
		{
			Key:   "_id",
			Value: id.String(),
		},
		{
			Key:   "revoked_at",
			Value: nil,
		},
	}

	update := bson.D{
		{
			Key: "$set",
			Value: bson.D{
				{
					Key:   "revoked_at",
					Value: revokedAt.UTC(),
				},
			},
		},
	}

	result, err := repository.collection.UpdateOne(
		ctx,
		filter,
		update,
	)
	if err != nil {
		return fmt.Errorf(
			"revoke session document: %w",
			err,
		)
	}

	if result.MatchedCount > 0 {
		return nil
	}

	exists, err := repository.existsByID(ctx, id)
	if err != nil {
		return err
	}

	if !exists {
		return ports.ErrSessionNotFound
	}

	return nil
}

func (repository *Repository) existsByID(
	ctx context.Context,
	id session.ID,
) (bool, error) {
	var marker struct {
		ID string `bson:"_id"`
	}

	err := repository.collection.FindOne(
		ctx,
		bson.D{
			{Key: "_id", Value: id.String()},
		},
		options.FindOne().SetProjection(
			bson.D{
				{Key: "_id", Value: 1},
			},
		),
	).Decode(&marker)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}

		return false, fmt.Errorf(
			"check session existence: %w",
			err,
		)
	}

	return true, nil
}
