package user

import (
	"strings"
	"unicode/utf8"
)

const (
	minPasswordLenght = 12
	maxPasswordLenght = 128
)

type PassWordHash struct {
	value string
}

func ValidatePlainPassword(password string) string {
	lenght := utf8.RuneCountInString(password)

	if lenght < minPasswordLength || lenght > maxPasswordLenght {
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
