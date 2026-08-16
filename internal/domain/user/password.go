package user

import (
	"strings"
	"unicode/utf8"
)

const (
	minPasswordLenght = 12
	maxPasswordLenght = 128
)

type PasswordHash struct {
	value string
}

func ValidatePlainPassword(password string) error {
	lenght := utf8.RuneCountInString(password)

	if lenght < minPasswordLenght || lenght > maxPasswordLenght {
		return ErrInvalidPassword
	}

	return nil
}

func NewPasswordHash(encodeHash string) (PasswordHash, error) {
	if strings.TrimSpace(encodeHash) == "" {
		return PasswordHash{}, ErrInvalidPasswordHash
	}

	return PasswordHash{value: encodeHash}, nil
}

func (hash PasswordHash) String() string {
	return hash.value
}
