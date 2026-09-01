package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const allowedOriginsEnvironment = "HTTP_ALLOWED_ORIGINS"

func LoadAllowedOrigins() ([]string, error) {
	rawValue := strings.TrimSpace(
		os.Getenv(allowedOriginsEnvironment),
	)
	if rawValue == "" {
		return nil, fmt.Errorf(
			"%w: %s is required",
			ErrInvalidCORSConfig,
			allowedOriginsEnvironment,
		)
	}

	uniqueOrigins := make(map[string]struct{})
	origins := make([]string, 0)

	for _, candidate := range strings.Split(rawValue, ",") {
		origin, err := normalizeOrigin(candidate)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid origin %q: %w",
				ErrInvalidCORSConfig,
				strings.TrimSpace(candidate),
				err,
			)
		}

		if _, alreadyIncluded := uniqueOrigins[origin]; alreadyIncluded {
			continue
		}

		uniqueOrigins[origin] = struct{}{}
		origins = append(origins, origin)
	}

	if len(origins) == 0 {
		return nil, fmt.Errorf(
			"%w: at least one origin is required",
			ErrInvalidCORSConfig,
		)
	}

	return origins, nil
}

func normalizeOrigin(rawOrigin string) (string, error) {
	rawOrigin = strings.TrimSpace(rawOrigin)
	if rawOrigin == "" {
		return "", fmt.Errorf("origin cannot be empty")
	}

	if rawOrigin == "*" {
		return "", fmt.Errorf(
			"wildcard origin cannot be used with credentialed requests",
		)
	}

	parsedOrigin, err := url.Parse(rawOrigin)
	if err != nil {
		return "", fmt.Errorf("parse origin: %w", err)
	}

	scheme := strings.ToLower(parsedOrigin.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("scheme must be http or https")
	}

	if parsedOrigin.Hostname() == "" {
		return "", fmt.Errorf("hostname is required")
	}

	if parsedOrigin.User != nil {
		return "", fmt.Errorf("credentials are not allowed")
	}

	if parsedOrigin.Path != "" && parsedOrigin.Path != "/" {
		return "", fmt.Errorf("path is not allowed")
	}

	if parsedOrigin.RawQuery != "" {
		return "", fmt.Errorf("query string is not allowed")
	}

	if parsedOrigin.Fragment != "" {
		return "", fmt.Errorf("fragment is not allowed")
	}

	port := parsedOrigin.Port()
	if port != "" {
		numericPort, err := strconv.Atoi(port)
		if err != nil || numericPort < 1 || numericPort > 65535 {
			return "", fmt.Errorf("port must be between 1 and 65535")
		}
	}

	if (scheme == "http" && port == "80") ||
		(scheme == "https" && port == "443") {
		port = ""
	}

	host := strings.ToLower(parsedOrigin.Hostname())

	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {

		host = "[" + host + "]"
	}

	return scheme + "://" + host, nil
}
