package userrepository

import (
	"context"
	"errors"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const userEmailUniqueIndexName = "users_email_unique"

var _ ports.UserRepository = (*Repository)(nil)

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
	index := mongo.IndexModel{
		Keys: bson.D{
			{Key: "email", Value: 1},
		},
		Options: options.Index().
			SetName(userEmailUniqueIndexName).
			SetUnique(true),
	}

	if _, err := repository.collection.
		Indexes().
		CreateOne(ctx, index); err != nil {
		return fmt.Errorf(
			"create unique user email index: %w",
			err,
		)
	}

	return nil
}

func (repository *Repository) Create(
	ctx context.Context,
	account *user.User,
) error {
	document, err := newUserDocument(account)
	if err != nil {
		return err
	}

	if _, err := repository.collection.InsertOne(
		ctx,
		document,
	); err != nil {
		if isEmailDuplicateError(err) {
			return ports.ErrEmailAlreadyExists
		}

		return fmt.Errorf(
			"insert user document: %w",
			err,
		)
	}

	return nil
}

func (repository *Repository) FindByEmail(
	ctx context.Context,
	email user.Email,
) (*user.User, error) {
	if email.String() == "" {
		return nil, user.ErrInvalidEmail
	}

	var document userDocument

	err := repository.collection.FindOne(
		ctx,
		bson.D{
			{Key: "email", Value: email.String()},
		},
	).Decode(&document)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ports.ErrUserNotFound
		}

		return nil, fmt.Errorf(
			"find user document by email: %w",
			err,
		)
	}

	account, err := document.toDomain()
	if err != nil {
		return nil, fmt.Errorf(
			"convert user document to domain: %w",
			err,
		)
	}

	return account, nil
}

func (repository *Repository) FindByID(
	ctx context.Context,
	id user.ID,
) (*user.User, error) {
	if id.String() == "" {
		return nil, user.ErrInvalidID
	}

	var document userDocument

	err := repository.collection.FindOne(
		ctx,
		bson.D{
			{Key: "_id", Value: id.String()},
		},
	).Decode(&document)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ports.ErrUserNotFound
		}

		return nil, fmt.Errorf(
			"find user document by id: %w",
			err,
		)
	}

	account, err := document.toDomain()
	if err != nil {
		return nil, fmt.Errorf(
			"convert user document to domain: %w",
			err,
		)
	}

	return account, nil
}

func isEmailDuplicateError(err error) bool {
	if !mongo.IsDuplicateKeyError(err) {
		return false
	}

	var serverError mongo.ServerError
	if !errors.As(err, &serverError) {
		return false
	}

	return serverError.HasErrorMessage(
		userEmailUniqueIndexName,
	)
}
