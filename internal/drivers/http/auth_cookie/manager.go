package authcookie

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
)

type Config struct {
	AccessName  string
	RefreshName string
	Path        string
	Secure      bool
	SameSite    http.SameSite
}

type Manager struct {
	config Config
	clock  ports.Clock
}

func NewManager(config Config, clock ports.Clock) (*Manager, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: clock", ErrNilDependency)
	}
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	return &Manager{
		config: config,
		clock:  clock,
	}, nil
}

func (manager *Manager) SetTokens(writer http.ResponseWriter, accessToken string, accessTokenExpireAt time.Time, refreshToken string, refreshExpiresAt time.Time) error {
	if strings.TrimSpace(accessToken) == "" || strings.TrimSpace(refreshToken) == "" {
		return ErrEmptyToken
	}

	now := manager.clock.Now().UTC()

	accessCookie, err := manager.newCookie(manager.config.AccessName, accessToken, accessTokenExpireAt, now)
	if err != nil {
		return err
	}

	refreshCookie, err := manager.newCookie(manager.config.RefreshName, refreshToken, refreshExpiresAt, now)
	if err != nil {
		return err
	}

	http.SetCookie(writer, accessCookie)
	http.SetCookie(writer, refreshCookie)
	writer.Header().Set("Cache-Control", "no-store")

	return nil
}

func (manager *Manager) AccessToken(httpRequest *http.Request) (string, error) {
	return readUniqueCookie(httpRequest, manager.config.AccessName)

}

func (manager *Manager) RefreshToken(httpRequest *http.Request) (string, error) {
	return readUniqueCookie(httpRequest, manager.config.RefreshName)
}

func (manager *Manager) Clear(writer http.ResponseWriter) error {
	expiredAt := time.Unix(1, 0).UTC()

	accessCookie := manager.expiredCookie(manager.config.AccessName, expiredAt)
	refreshCookie := manager.expiredCookie(manager.config.RefreshName, expiredAt)

	if err := accessCookie.Valid(); err != nil {
		return fmt.Errorf("%w: access cookie: %v",
			ErrInvalidConfig,
			err)
	}

	if err := refreshCookie.Valid(); err != nil {
		return fmt.Errorf(
			"%w: refresh cookie: %v",
			ErrInvalidConfig,
			err,
		)
	}
	http.SetCookie(writer, accessCookie)
	http.SetCookie(writer, refreshCookie)
	writer.Header().Set("Cache-Control", "no-store")
	return nil
}

func (manager *Manager) newCookie(name string, token string, expiresAt time.Time, now time.Time) (*http.Cookie, error) {
	maxAge, err := calculateMaxAge(expiresAt, now)
	if err != nil {
		return nil, err
	}

	cookie := &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     manager.config.Path,
		Domain:   "",
		Expires:  expiresAt.UTC(),
		MaxAge:   maxAge,
		Secure:   manager.config.Secure,
		HttpOnly: true,
		SameSite: manager.config.SameSite,
	}

	if err := cookie.Valid(); err != nil {
		return nil, fmt.Errorf("%w: %v",
			ErrInvalidConfig,
			err)
	}

	return cookie, nil
}

func (manager *Manager) expiredCookie(name string, expiredAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     manager.config.Path,
		Domain:   "",
		Expires:  expiredAt,
		MaxAge:   -1,
		Secure:   manager.config.Secure,
		HttpOnly: true,
		SameSite: manager.config.SameSite,
	}
}

func readUniqueCookie(httpRequest *http.Request, name string) (string, error) {
	if httpRequest == nil {
		return "", ErrCookieNotFound
	}
	var token string
	matches := 0

	for _, cookie := range httpRequest.Cookies() {
		if cookie.Name != name {
			continue
		}

		matches++
		token = cookie.Value
	}

	if matches == 0 || strings.TrimSpace(token) == "" {
		return "", ErrCookieNotFound
	}

	if matches > 1 {
		return "", ErrDuplicateCookie
	}
	return token, nil
}

func calculateMaxAge(expiresAt time.Time, now time.Time) (int, error) {
	duration := expiresAt.UTC().Sub(now.UTC())
	if duration <= 0 {
		return 0, ErrInvalidExpiration
	}

	seconds := int(math.Ceil(duration.Seconds()))
	if seconds <= 0 {
		return 0, ErrInvalidExpiration
	}
	return seconds, nil
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.AccessName) == "" || strings.TrimSpace(config.RefreshName) == "" {
		return fmt.Errorf("%w: cookie names cannot be empty", ErrInvalidConfig)

	}

	if config.AccessName == config.RefreshName {
		return fmt.Errorf("%: cookie names must be different", ErrInvalidConfig)
	}
	if config.Path != "/" {
		return fmt.Errorf("%w: cookie path must be /", ErrInvalidConfig)
	}
	switch config.SameSite {
	case http.SameSiteStrictMode,
		http.SameSiteLaxMode,
		http.SameSiteNoneMode:

	default:
		return fmt.Errorf("%w: unsupported SameSite mode", ErrInvalidConfig)
	}

	if config.SameSite == http.SameSiteNoneMode && !config.Secure {
		return fmt.Errorf("%w: SameSite=None requires Secure", ErrInvalidConfig)
	}

	if config.Secure {
		if !strings.HasPrefix(config.AccessName, "__Host-") {
			return fmt.Errorf(
				"%w: secure access cookie must use __Host-",
				ErrInvalidConfig,
			)
		}
	}

	if config.Secure && !strings.HasPrefix(config.RefreshName, "__Host-") {
		return fmt.Errorf(
			"%w: secure refresh cookie must use __Host-",
			ErrInvalidConfig,
		)
	}
	validationCookies := []*http.Cookie{
		{
			Name:     config.AccessName,
			Value:    "validation",
			Path:     config.Path,
			Secure:   config.Secure,
			HttpOnly: true,
			SameSite: config.SameSite,
		},
		{
			Name:     config.RefreshName,
			Value:    "validation",
			Path:     config.Path,
			Secure:   config.Secure,
			HttpOnly: true,
			SameSite: config.SameSite,
		},
	}

	for _, cookie := range validationCookies {
		if err := cookie.Valid(); err != nil {
			return fmt.Errorf(
				"%w: %v",
				ErrInvalidConfig,
				err,
			)
		}
	}

	return nil
}
