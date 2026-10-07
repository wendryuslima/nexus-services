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

const (
	userEmailUniqueIndexName = "users_email_unique"
	userListIndexName        = "users_created_at_id"
)

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
	indexes := []mongo.IndexModel{{
		Keys: bson.D{
			{Key: "email", Value: 1},
		},
		Options: options.Index().
			SetName(userEmailUniqueIndexName).
			SetUnique(true),
	}, {
		Keys:    bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}},
		Options: options.Index().SetName(userListIndexName),
	}}

	if _, err := repository.collection.
		Indexes().
		CreateMany(ctx, indexes); err != nil {
		return fmt.Errorf(
			"create user indexes: %w",
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

func (repository *Repository) FindByIDs(ctx context.Context, ids []user.ID) ([]*user.User, error) {
	accounts := make([]*user.User, 0, len(ids))
	if len(ids) == 0 {
		return accounts, nil
	}
	values := make(bson.A, 0, len(ids))
	for _, id := range ids {
		if id.String() == "" {
			return nil, user.ErrInvalidID
		}
		values = append(values, id.String())
	}
	cursor, err := repository.collection.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: values}}}})
	if err != nil {
		return nil, fmt.Errorf("find user documents by ids: %w", err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var document userDocument

		if err := cursor.Decode(&document); err != nil {
			return nil, fmt.Errorf("decode user document: %w", err)
		}
		account, err := document.toDomain()
		if err != nil {
			return nil, fmt.Errorf("convert user document to domain: %w", err)
		}
		accounts = append(accounts, account)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("iterate over user documents: %w", err)

	}

	return accounts, nil
}

func (repository *Repository) List(ctx context.Context, params ports.ListUsersParams) (ports.ListUsersResult, error) {
	if params.Limit < 1 || params.Limit > 100 {
		return ports.ListUsersResult{}, fmt.Errorf("invalid user list limit")
	}
	filter := bson.D{}
	if params.After != nil {
		if params.After.ID.String() == "" || params.After.CreatedAt.IsZero() {
			return ports.ListUsersResult{}, fmt.Errorf("invalid user list cursor")
		}
		filter = bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "created_at", Value: bson.D{{Key: "$gt", Value: params.After.CreatedAt.UTC()}}}},
			bson.D{{Key: "created_at", Value: params.After.CreatedAt.UTC()}, {Key: "_id", Value: bson.D{{Key: "$gt", Value: params.After.ID.String()}}}},
		}}}
	}
	totalItems, err := repository.collection.CountDocuments(ctx, bson.D{})
	if err != nil {
		return ports.ListUsersResult{}, fmt.Errorf("count user documents: %w", err)
	}
	cursor, err := repository.collection.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}).
		SetLimit(int64(params.Limit+1)))
	if err != nil {
		return ports.ListUsersResult{}, fmt.Errorf("find user page: %w", err)
	}
	defer cursor.Close(ctx)
	accounts := make([]*user.User, 0, params.Limit+1)
	for cursor.Next(ctx) {
		var document userDocument
		if err := cursor.Decode(&document); err != nil {
			return ports.ListUsersResult{}, fmt.Errorf("decode user page: %w", err)
		}
		account, err := document.toDomain()
		if err != nil {
			return ports.ListUsersResult{}, fmt.Errorf("convert user page: %w", err)
		}
		accounts = append(accounts, account)
	}
	if err := cursor.Err(); err != nil {
		return ports.ListUsersResult{}, fmt.Errorf("iterate user page: %w", err)
	}
	hasNext := len(accounts) > params.Limit
	if hasNext {
		accounts = accounts[:params.Limit]
	}
	var nextCursor *ports.UserListCursor
	if hasNext {
		last := accounts[len(accounts)-1]
		nextCursor = &ports.UserListCursor{ID: last.ID(), CreatedAt: last.CreatedAt()}
	}
	return ports.ListUsersResult{Users: accounts, HasNext: hasNext, NextCursor: nextCursor, TotalItems: totalItems}, nil
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
