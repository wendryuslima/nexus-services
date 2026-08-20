package jwtadapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

const (
	minimumHMACKeyLength = 32
	maximumLeeway        = 2 * time.Minute
)

var _ ports.TokenManager = (*Manager)(nil)

type Config struct {
	Issuer        string
	Audience      string
	AccessSecret  []byte
	RefreshSecret []byte
	Leeway        time.Duration
}

type Manager struct {
	issuer        string
	audience      string
	accessSecret  []byte
	refreshSecret []byte
	parser        *jwtlib.Parser
}

func NewManager(
	config Config,
	clock ports.Clock,
) (*Manager, error) {
	if clock == nil {
		return nil, fmt.Errorf(
			"%w: clock is nil",
			ErrInvalidConfiguration,
		)
	}

	if strings.TrimSpace(config.Issuer) == "" {
		return nil, fmt.Errorf(
			"%w: issuer is empty",
			ErrInvalidConfiguration,
		)
	}

	if strings.TrimSpace(config.Audience) == "" {
		return nil, fmt.Errorf(
			"%w: audience is empty",
			ErrInvalidConfiguration,
		)
	}

	if len(config.AccessSecret) < minimumHMACKeyLength {
		return nil, fmt.Errorf(
			"%w: access secret must have at least %d bytes",
			ErrInvalidConfiguration,
			minimumHMACKeyLength,
		)
	}

	if len(config.RefreshSecret) < minimumHMACKeyLength {
		return nil, fmt.Errorf(
			"%w: refresh secret must have at least %d bytes",
			ErrInvalidConfiguration,
			minimumHMACKeyLength,
		)
	}

	if bytes.Equal(config.AccessSecret, config.RefreshSecret) {
		return nil, fmt.Errorf(
			"%w: access and refresh secrets must be different",
			ErrInvalidConfiguration,
		)
	}

	if config.Leeway < 0 || config.Leeway > maximumLeeway {
		return nil, fmt.Errorf(
			"%w: leeway must be between zero and %s",
			ErrInvalidConfiguration,
			maximumLeeway,
		)
	}

	accessSecret := append([]byte(nil), config.AccessSecret...)
	refreshSecret := append([]byte(nil), config.RefreshSecret...)

	parser := jwtlib.NewParser(
		jwtlib.WithValidMethods(
			[]string{jwtlib.SigningMethodHS256.Alg()},
		),
		jwtlib.WithIssuer(config.Issuer),
		jwtlib.WithAudience(config.Audience),
		jwtlib.WithExpirationRequired(),
		jwtlib.WithNotBeforeRequired(),
		jwtlib.WithIssuedAt(),
		jwtlib.WithLeeway(config.Leeway),
		jwtlib.WithStrictDecoding(),
		jwtlib.WithTimeFunc(func() time.Time {
			return clock.Now().UTC()
		}),
	)

	return &Manager{
		issuer:        config.Issuer,
		audience:      config.Audience,
		accessSecret:  accessSecret,
		refreshSecret: refreshSecret,
		parser:        parser,
	}, nil
}

func (manager *Manager) SignAccessToken(
	ctx context.Context,
	claims ports.TokenClaims,
) (string, error) {
	return manager.sign(
		ctx,
		claims,
		accessTokenType,
		manager.accessSecret,
	)
}

func (manager *Manager) SignRefreshToken(
	ctx context.Context,
	claims ports.TokenClaims,
) (string, error) {
	return manager.sign(
		ctx,
		claims,
		refreshTokenType,
		manager.refreshSecret,
	)
}

func (manager *Manager) VerifyAccessToken(
	ctx context.Context,
	rawToken string,
) (ports.TokenClaims, error) {
	return manager.verify(
		ctx,
		rawToken,
		accessTokenType,
		manager.accessSecret,
	)
}

func (manager *Manager) VerifyRefreshToken(
	ctx context.Context,
	rawToken string,
) (ports.TokenClaims, error) {
	return manager.verify(
		ctx,
		rawToken,
		refreshTokenType,
		manager.refreshSecret,
	)
}

func (manager *Manager) sign(
	ctx context.Context,
	claims ports.TokenClaims,
	tokenType string,
	secret []byte,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if err := validateTokenType(tokenType); err != nil {
		return "", err
	}

	if err := validateSigninClaims(claims); err != nil {
		return "", err
	}

	jwtClaims := newTokenClaims(
		claims,
		tokenType,
		manager.issuer,
		manager.audience,
	)

	token := jwtlib.NewWithClaims(
		jwtlib.SigningMethodHS256,
		jwtClaims,
	)

	token.Header["typ"] = "JWT"

	signedToken, err := token.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf(
			"sign %s token: %w",
			tokenType,
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return signedToken, nil
}

func (manager *Manager) verify(
	ctx context.Context,
	rawToken string,
	expectedTokenType string,
	secret []byte,
) (ports.TokenClaims, error) {
	if err := ctx.Err(); err != nil {
		return ports.TokenClaims{}, err
	}

	if strings.TrimSpace(rawToken) == "" {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	if err := validateTokenType(expectedTokenType); err != nil {
		return ports.TokenClaims{}, err
	}

	parsedClaims := &tokenClaims{}

	token, err := manager.parser.ParseWithClaims(
		rawToken,
		parsedClaims,
		func(token *jwtlib.Token) (any, error) {

			if token.Method != jwtlib.SigningMethodHS256 {
				return nil, ports.ErrInvalidToken
			}

			return secret, nil
		},
	)
	if err != nil {
		if errors.Is(err, jwtlib.ErrTokenExpired) {
			return ports.TokenClaims{}, ports.ErrExpiredToken
		}

		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	if token == nil || !token.Valid {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	claims, err := toPortClaims(
		*parsedClaims,
		expectedTokenType,
	)
	if err != nil {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}

	if err := ctx.Err(); err != nil {
		return ports.TokenClaims{}, err
	}

	return claims, nil
}
