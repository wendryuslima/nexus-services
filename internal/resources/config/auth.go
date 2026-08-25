package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	applicationEnvironmentVariable = "APP_ENV"
	accessTokenTTLEnvironment      = "AUTH_ACCESS_TOKEN_TTL"
	refreshTokenTTLEnvironment     = "AUTH_REFRESH_TOKEN_TTL"
	jwtIssuerEnvironment           = "JWT_ISSUER"
	jwtAudienceEnvironment         = "JWT_AUDIENCE"
	jwtAccessSecretEnvironment     = "JWT_ACESS_SECRET_BASE64"
	jwtRefreshSecretEnvironment    = "JWT_REFRESH_SECRET_BASE64"
	jwtLeewayEnvironment           = "JWT_LEEWAY"
	cookieSecureEnvironment        = "AUTH_COOKIE_SECURE"
	cookieSameSiteEnvironment      = "AUTH_COOKIE_SAME_SITE"

	defaultAccessTokenTTL  = 15 * time.Minute
	defaultRefreshTokenTTL = 7 * 24 * time.Hour
	defaultJWTLeeway       = 30 * time.Second

	minimumAccessTokenTTL  = time.Minute
	maximumAccessTokenTTL  = time.Hour
	minimumRefreshTokenTTL = time.Hour
	maximumRefreshTokenTTL = 30 * 24 * time.Hour
	maximumJWTLeeway       = 2 * time.Minute

	minimumJWTSecretLength = 32

	productionAccessCookieName  = "__Host-nexus-access"
	productionRefreshCookieName = "__Host-nexus-refresh"
	localAccessCookieName       = "nexus-access"
	localRefreshCookieName      = "nexus-refresh"

	authenticationCookiePath = "/"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)

type SameSite string

const (
	SameSiteStrict SameSite = "strict"
	SameSiteLax    SameSite = "lax"
	SameSiteNone   SameSite = "none"
)

type JWTConfig struct {
	Issuer        string
	Audience      string
	AccessSecret  []byte
	RefreshSecret []byte
	Leeway        time.Duration
}

type CookieConfig struct {
	AccessName  string
	RefreshName string
	Path        string
	Secure      bool
	SameSite    SameSite
}

type AuthConfig struct {
	Environment     Environment
	AccessTokenTTL  time.Duration
	RefreshTOkenTTL time.Duration
	JWT             JWTConfig
	Cookies         CookieConfig
}

func LoadAuthConfig() (AuthConfig, error) {
	environmentValue, err := readRequiredEnvironment(applicationEnvironmentVariable)
	if err != nil {
		return AuthConfig{}, err
	}
	environment, err := parseEnvironment(environmentValue)
	if err != nil {
		return AuthConfig{}, err
	}

	accessTokenTTL, err := readBoundedDurationEnvironment(accessTokenTTLEnvironment, defaultAccessTokenTTL, minimumAccessTokenTTL, maximumAccessTokenTTL)
	if err != nil {
		return AuthConfig{}, err
	}

	refreshTokenTTL, err := readBoundedDurationEnvironment(refreshTokenTTLEnvironment, defaultRefreshTokenTTL, minimumRefreshTokenTTL, maximumRefreshTokenTTL)
	if err != nil {
		return AuthConfig{}, err
	}

	issuer, err := readRequiredEnvironment(jwtIssuerEnvironment)
	if err != nil {
		return AuthConfig{}, err
	}

	audience, err := readRequiredEnvironment(jwtAudienceEnvironment)
	if err != nil {
		return AuthConfig{}, err
	}

	accessSecret, err := readBase64Secret(jwtAccessSecretEnvironment)
	if err != nil {
		return AuthConfig{}, err
	}

	refreshSecret, err := readBase64Secret(jwtRefreshSecretEnvironment)
	if err != nil {
		return AuthConfig{}, err
	}

	leeway, err := readBoundedDurationEnvironment(jwtLeewayEnvironment, defaultJWTLeeway, 0, maximumJWTLeeway)
	if err != nil {
		return AuthConfig{}, err
	}

	defaultSecure := environment.requiresSecureCookies()

	cookieSecure, err := readBooleanEnvironment(cookieSecureEnvironment, defaultSecure)
	if err != nil {
		return AuthConfig{}, err
	}
	sameSite, err := readSameSiteEnvironment(cookieSameSiteEnvironment, SameSiteStrict)
	if err != nil {
		return AuthConfig{}, err
	}

	config := AuthConfig{
		Environment:     environment,
		AccessTokenTTL:  accessTokenTTL,
		RefreshTOkenTTL: refreshTokenTTL,
		JWT: JWTConfig{
			Issuer:        issuer,
			Audience:      audience,
			AccessSecret:  accessSecret,
			RefreshSecret: refreshSecret,
			Leeway:        leeway,
		},
		Cookies: buildCookieConfig(
			cookieSecure,
			sameSite,
		),
	}

	if err := config.Validate(); err != nil {
		return AuthConfig{}, err
	}

	return config, nil
}

func (config AuthConfig) Validate() error {
	if !config.Environment.isValid() {
		return fmt.Errorf("%w: unknown environment", ErrInvalidAuthConfig)
	}

	if config.AccessTokenTTL < maximumAccessTokenTTL ||
		config.AccessTokenTTL > maximumAccessTokenTTL {
		return fmt.Errorf("%w: access token TTL is outside allowed limits", ErrInvalidAuthConfig)

	}
	if config.RefreshTOkenTTL < minimumRefreshTokenTTL ||
		config.RefreshTOkenTTL > maximumAccessTokenTTL ||
		config.RefreshTOkenTTL <= config.AccessTokenTTL {
		return fmt.Errorf("%w: refresh token TTL is outside allowed limits", ErrInvalidAuthConfig)
	}

	if strings.TrimSpace(config.JWT.Issuer) == "" {
		return fmt.Errorf("%w: JWT issuer is empty", ErrInvalidAuthConfig)
	}

	if strings.TrimSpace(config.JWT.Audience) == "" {
		return fmt.Errorf("%w: JWT audience is empty", ErrInvalidAuthConfig)
	}
	if len(config.JWT.AccessSecret) < minimumJWTSecretLength {
		return fmt.Errorf("%w: access secret must have at least %d bytes", ErrInvalidAuthConfig,
			minimumJWTSecretLength)

	}

	if len(config.JWT.RefreshSecret) < minimumJWTSecretLength {
		return fmt.Errorf(
			"%w: refresh secret must have at least %d bytes",
			ErrInvalidAuthConfig,
			minimumJWTSecretLength,
		)
	}

	if bytes.Equal(config.JWT.AccessSecret, config.JWT.RefreshSecret) {
		return fmt.Errorf("%w: access and refresh secrets must be different", ErrInvalidAuthConfig)
	}

	if config.JWT.Leeway < 0 || config.JWT.Leeway > maximumJWTLeeway {
		return fmt.Errorf("%w: JWT leeway is outside allowed limits", ErrInvalidAuthConfig)
	}

	if config.Environment.requiresSecureCookies() && !config.Cookies.Secure {
		return fmt.Errorf("%w: secure cookies are required in this environment", ErrInvalidAuthConfig)
	}

	if config.Cookies.Path != authenticationCookiePath {
		return fmt.Errorf("%w: authentication cookie path must be /", ErrInvalidAuthConfig)
	}
	if !config.Cookies.SameSite.isValid() {
		return fmt.Errorf("%w: invalid cookie SameSite policy", ErrInvalidAuthConfig)
	}

	if config.Cookies.SameSite == SameSiteNone && !config.Cookies.Secure {
		return fmt.Errorf("%w: SameSite=None requires Secure", ErrInvalidAuthConfig)
	}

	if config.Cookies.Secure {
		if !strings.HasPrefix(config.Cookies.AccessName, "__Host-") {
			return fmt.Errorf("%w: secure access cookie must use __Host-", ErrInvalidAuthConfig)
		}
	}

	if !strings.HasPrefix(config.Cookies.RefreshName, "__Host-") {
		return fmt.Errorf("%w: secure refresh cookie must use __Host-", ErrInvalidAuthConfig)
	}

	if config.Cookies.AccessName == config.Cookies.RefreshName {
		return fmt.Errorf("%w: cookie names must be different", ErrInvalidAuthConfig)
	}
	return nil
}

func parseEnvironment(value string) (Environment, error) {
	environment := Environment(strings.ToLower(strings.TrimSpace(value)))
	if !environment.isValid() {
		return "", fmt.Errorf("%w: %s", ErrInvalidEnvironmentVariable, applicationEnvironmentVariable)
	}

	return environment, nil
}

func (environment Environment) isValid() bool {
	switch environment {
	case EnvironmentDevelopment,
		EnvironmentTest,
		EnvironmentStaging,
		EnvironmentProduction:
		return true
	default:
		return false
	}
}

func (environment Environment) requiresSecureCookies() bool {
	return environment == EnvironmentStaging ||
		environment == EnvironmentProduction
}

func (sameSite SameSite) isValid() bool {
	switch sameSite {
	case SameSiteStrict, SameSiteLax, SameSiteNone:
		return true
	default:
		return false
	}
}

func buildCookieConfig(secure bool, sameSite SameSite) CookieConfig {
	if secure {
		return CookieConfig{
			AccessName:  productionAccessCookieName,
			RefreshName: productionRefreshCookieName,
			Path:        authenticationCookiePath,
			Secure:      true,
			SameSite:    sameSite,
		}
	}

	return CookieConfig{
		AccessName:  localAccessCookieName,
		RefreshName: localRefreshCookieName,
		Path:        authenticationCookiePath,
		Secure:      false,
		SameSite:    sameSite,
	}
}

func readBase64Secret(name string) ([]byte, error) {
	encodeSecret, err := readRequiredEnvironment(name)
	if err != nil {
		return nil, err
	}
	secret, err := base64.StdEncoding.Strict().DecodeString(encodeSecret)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must contain valid Base64", ErrInvalidEnvironmentVariable, name)
	}

	if len(secret) < minimumJWTSecretLength {
		return nil, fmt.Errorf("%w: %s must decode to at least %d bytes", ErrInvalidEnvironmentVariable, name, minimumJWTSecretLength)
	}

	return secret, nil
}

func readBooleanEnvironment(name string, defaultValue bool) (bool, error) {
	rawValue, exists := os.LookupEnv(name)
	if !exists {
		return defaultValue, nil
	}

	switch strings.ToLower(strings.TrimSpace(rawValue)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%w: %s must be true or false", ErrInvalidEnvironmentVariable, name)
	}
}

func readSameSiteEnvironment(name string, defaultValue SameSite) (SameSite, error) {
	rawValue, exists := os.LookupEnv(name)
	if !exists {
		return defaultValue, nil
	}
	value := SameSite(strings.ToLower(strings.TrimSpace(rawValue)))

	if !value.isValid() {
		return "", fmt.Errorf(
			"%w: %s must be strict, lax or none",
			ErrInvalidEnvironmentVariable,
			name,
		)
	}

	return value, nil
}

func readBoundedDurationEnvironment(name string, defaultValue time.Duration, minimum time.Duration, maximum time.Duration) (time.Duration, error) {
	rawValue, exists := os.LookupEnv(name)
	if !exists {
		return defaultValue, nil
	}

	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return 0, fmt.Errorf("%w: %s cannot be empty", ErrInvalidEnvironmentVariable, name)
	}

	value, err := time.ParseDuration(rawValue)
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be a duration", ErrInvalidEnvironmentVariable, name)
	}

	if value < minimum || value > maximum {
		return 0, fmt.Errorf("%w: %s is outside allowed limits", ErrInvalidEnvironmentVariable, name)
	}

	return value, nil
}
