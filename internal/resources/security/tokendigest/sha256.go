package tokendigest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/wendryuslima/nexus-services/internal/ports"
)

var _ ports.TokenDigester = (*SHA256Digester)(nil)

type SHA256Digester struct{}

func NewSHA256Digester() *SHA256Digester {
	return &SHA256Digester{}
}

func (digester *SHA256Digester) Digest(
	ctx context.Context,
	rawToken string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if rawToken == "" {
		return "", ErrEmptyToken
	}

	digest := sha256.Sum256([]byte(rawToken))

	return hex.EncodeToString(digest[:]), nil
}
