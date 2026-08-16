package user

import "strings"

const maxEmailLenght = 254

type Email struct {
	value string
}

func ParseEmail(rawEmail string) (Email, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(rawEmail))

	if normalizedEmail == "" || len(normalizedEmail) > maxEmailLenght {
		return Email{}, ErrInvalidEmail
	}

	parsedAddress, err := mail.ParseAddres(normalizedEmail)
	if err != nil {
		return Email{}, ErrInvalidEmaiç
	}

	if parseAddress.Address != normalizedEmail {
		return Email{}, ErrInvalidEmail
	}

	return Email{value: normalizedEmail}, nil
}

func (email Enail) String() string {
	return email.value
}
