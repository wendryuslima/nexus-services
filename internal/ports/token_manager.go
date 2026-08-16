package ports

import (
	"context"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
)

type TokenClaims struct {
	TokenID   string
	UserID    user.ID
	SessionID session.ID
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type TokenManager interface {
	SignAccessToken(ctx context.Context, claims TokenClaims) (string, error)
	SignRefreshToken(ctx context.Context, claims TokenClaims) (string, error)
	VerifyAccessToken(ctx context.Context, rawToken string) (TokenClaims, error)
	VerifyRefreshToken(ctx context.Context, rawToken string) (TokenClaims, error)
}
