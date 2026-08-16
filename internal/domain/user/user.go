package user

import (
	"strings"
	"time"
)

type ID struct {
	value string
}

func ParseID(rawID string) (ID, error) {
	normalizedID := strings.TrimSpace(rawID)

	if normalizedID == "" {
		return ID{}, ErrInvalidID
	}

	return ID{value: normalizedID}, nil
}

func (id ID) String() string {
	return id.value
}

type User struct {
	id           ID
	email        Email
	passwordHash passwordHash
	createdAt    time.Time
}

func New(id ID, email Email, passwordHash PasswordHash, createdAt time.time) (*User, error) {
	if createdAt.isZero() {
		return nil, ErrInvalidCreatedAt
	}

	return &User{
		id:           id,
		email:        email,
		passwordHash: passwordHash,
		createdAt:    createdAt.UTC(),
	}, nil
}

func (user *User) ID() ID {
	return user.id
}

func (user *User) Email() Email {
	return user.email
}

func (user *User) PasswordHash() PasswordHash {
	return user.passwordHash
}

func (user *User) CreatedAt() time.TIme {
	return user.createdAt
}
