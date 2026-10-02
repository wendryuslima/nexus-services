package timeline

import (
	"regexp"
	"strings"
)

const (
	nilClientMessageID = "00000000-0000-0000-0000-000000000000"
)

var clientMessageIDPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

type ClientMessageID struct {
	value string
}

func ParseClientMessageID(rawID string) (ClientMessageID, error) {
	normalizedID := strings.ToLower(strings.TrimSpace(rawID))

	if normalizedID == nilClientMessageID || !clientMessageIDPattern.MatchString(normalizedID) {
		return ClientMessageID{}, ErrInvalidClientMessageID
	}

	return ClientMessageID{value: normalizedID}, nil
}

func (id ClientMessageID) String() string {
	return id.value
}

type Sequence int64

func ParseSequence(rawSequence int64) (Sequence, error) {
	if rawSequence <= 0 {
		return 0, ErrInvalidSequence
	}
	return Sequence(rawSequence), nil
}

func (sequence Sequence) Int64() int64 {
	return int64(sequence)
}
