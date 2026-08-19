package identifier

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/wendryuslima/nexus-services/internal/ports"
)

const uuidSize = 16

var _ ports.IDGenerator = (*UUIDGenerator)(nil)

type UUIDGenerator struct{}

func NewUUIDGenerator() *UUIDGenerator {
	return &UUIDGenerator{}
}

func (generator *UUIDGenerator) New(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	randomBytes := make([]byte, uuidSize)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("read random bytes for UUID: %w", err)
	}

	randomBytes[8] = (randomBytes[8] & 0x3f) | 0x80

	return formatUUID(randomBytes), nil
}

func formatUUID(value []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	)
}
