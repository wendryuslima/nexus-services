package ports

import (
	"context"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/session"
)

type SessionRepository interface {
	Create(
		ctx context.Context,
		authSession *session.Session,
	) error

	FindByID(
		ctx context.Context,
		id session.ID,
	) (*session.Session, error)

	Rotate(
		ctx context.Context,
		authSession *session.Session,
		expectedDigest string,
		rotatedAt time.Time,
	) error

	Revoke(
		ctx context.Context,
		id session.ID,
		revokedAt time.Time,
	) error
}
