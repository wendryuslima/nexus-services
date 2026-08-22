package sessionrepository

import (
	"fmt"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type sessionDocument struct {
	ID                 string     `bson:"_id"`
	UserID             string     `bson:"user_id"`
	RefreshTokenDigest string     `bson:"refresh_token_digest"`
	CreatedAt          time.Time  `bson:"created_at"`
	ExpiresAt          time.Time  `bson:"expires_at"`
	RevokedAt          *time.Time `bson:"revoked_at"`
}

func newSessionDocument(authSession *session.Session) (sessionDocument, error) {
	if authSession == nil {
		return sessionDocument{}, ErrNilSession
	}

	var revokedAt *time.Time

	if value, exists := authSession.RevokedAt(); exists {
		normalizedValue := value.UTC()
		revokedAt = &normalizedValue
	}

	return sessionDocument{
		ID:                 authSession.ID().String(),
		UserID:             authSession.UserID().String(),
		RefreshTokenDigest: authSession.RefreshTokenDigest(),
		CreatedAt:          authSession.CreatedAt().UTC(),
		ExpiresAt:          authSession.ExpiresAt().UTC(),
		RevokedAt:          revokedAt,
	}, nil
}

func (document sessionDocument) toDomain() (*session.Session, error) {
	sessionID, err := session.ParseID(document.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: parse session id: %v", ErrNilSession, err)
	}

	userID, err := user.ParseID(document.UserID)
	if err != nil {
		return nil, fmt.Errorf("%w: parse session user id: %v", ErrInvalidDocument, err)
	}

	authSession, err := session.Restore(sessionID, userID, document.RefreshTokenDigest, document.CreatedAt, document.ExpiresAt, document.RevokedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: restore session: %v", ErrInvalidDocument, err)
	}

	return authSession, nil
}
