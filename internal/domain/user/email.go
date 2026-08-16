package user

import (
	"net/mail"
	"strings"
)

const maxEmailLenght = 254

type Email struct {
	value string
}

func ParseEmail(rawEmail string) (Email, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(rawEmail))

	if normalizedEmail == "" || len(normalizedEmail) > maxEmailLenght {
		return Email{}, ErrInvalidEmail
	}

	parsedAddress, err := mail.ParseAddress(normalizedEmail)
	if err != nil {
		return Email{}, ErrInvalidEmail
	}

	if parsedAddress.Address != normalizedEmail {
		return Email{}, ErrInvalidEmail
	}

	return Email{value: normalizedEmail}, nil
}

func (email Email) String() string {
	return email.value
}
