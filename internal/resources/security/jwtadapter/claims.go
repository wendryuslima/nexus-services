package jwtadapter

import (
	"fmt"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	accessTokenType  = "access"
	refreshTokenType = "refresh"
)

type tokenClaims struct {
	TokenType string `json:"token_type"`
	SessionID string `json:"sid"`
	jwtlib.RegisteredClaims
}

func newTokenClaims(claims ports.TokenClaims, tokenType string, issuer string, audience string) tokenClaims {
	return tokenClaims{
		TokenType: tokenType,
		SessionID: claims.SessionID.String(),
		RegisteredClaims: jwtlib.RegisteredClaims{
			Issuer:    issuer,
			Subject:   claims.UserID.String(),
			Audience:  jwtlib.ClaimStrings{audience},
			ExpiresAt: jwtlib.NewNumericDate(claims.ExpiresAt.UTC()),
			NotBefore: jwtlib.NewNumericDate(claims.IssuedAt.UTC()),
			IssuedAt:  jwtlib.NewNumericDate(claims.IssuedAt.UTC()),
			ID:        claims.TokenID,
		},
	}
}

func validateSigninClaims(claims ports.TokenClaims) error {
	if strings.TrimSpace(claims.TokenID) == "" {
		return fmt.Errorf("%w: token id is empty", ErrInvalidClaims)
	}

	if claims.UserID.String() == "" {
		return fmt.Errorf("%w: user id is empty", ErrInvalidClaims)
	}

	if claims.SessionID.String() == "" {
		return fmt.Errorf("%w: session id is empty", ErrInvalidClaims)
	}

	if claims.IssuedAt.IsZero() {
		return fmt.Errorf("%w: issued-at is empty", ErrInvalidClaims)
	}

	if claims.ExpiresAt.IsZero() || !claims.ExpiresAt.After(claims.IssuedAt) {
		return fmt.Errorf("%w: expiration must be after issued-at", ErrInvalidClaims)
	}

	return nil
}

func toPortClaims(claims tokenClaims, expectedTokenType string) (ports.TokenClaims, error) {
	if claims.TokenType != expectedTokenType {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	if strings.TrimSpace(claims.ID) == "" || strings.TrimSpace(claims.Subject) == "" ||
		strings.TrimSpace(claims.SessionID) == "" {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	if claims.IssuedAt == nil ||
		claims.NotBefore == nil ||
		claims.ExpiresAt == nil {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}
	if !claims.ExpiresAt.Time.After(claims.IssuedAt.Time) {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}
	userID, err := user.ParseID(claims.Subject)
	if err != nil {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	sessionID, err := session.ParseID(claims.SessionID)
	if err != nil {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	return ports.TokenClaims{
		TokenID:   claims.ID,
		UserID:    userID,
		SessionID: sessionID,
		IssuedAt:  claims.IssuedAt.Time.UTC(),
		ExpiresAt: claims.ExpiresAt.Time.UTC(),
	}, nil

}

func validateTokenType(tokenType string) error {
	switch tokenType {
	case accessTokenType, refreshTokenType:
		return nil

	default:
		return fmt.Errorf(
			"%w: unknown token type",
			ErrInvalidClaims,
		)
	}
}

func normalizeTime(value time.Time) time.Time {
	return value.UTC()
}
