package userrepository

import (
	"fmt"

	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type userDocument struct {
	ID           string    `bson:"_id"`
	Email        string    `bson:"email"`
	PasswordHash string    `bson:"password_hash"`
	CreatedAt    time.Time `bson:"created_at"`
}

func newUserDocument(account *user.User) (userDocument, error) {
	if account == nil {
		return userDocument{}, ErrNilUser
	}

	return userDocument{
		ID:           account.ID().String(),
		Email:        account.Email().String(),
		PasswordHash: account.PasswordHash().String(),
		CreatedAt:    account.CreatedAt().UTC(),
	}, nil
}

func (document userDocument) toDomain() (*user.User, error) {
	userID, err := user.ParseID(document.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: parse user id: %v", ErrInvalidDocument, err)
	}
	email, err := user.ParseEmail(document.Email)
	if err != nil {
		return nil, fmt.Errorf("%w: parse email: %v", ErrInvalidDocument, err)
	}
	passwordHash, err := user.NewPasswordHash(document.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("%w: parse password hash: %v", ErrInvalidDocument, err)
	}
	account, err := user.New(userID, email, passwordHash, document.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: restore user: %v", ErrInvalidDocument, err)
	}

	return account, nil
}
