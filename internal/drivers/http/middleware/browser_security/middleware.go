package browsersecurity

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
)

const (
	maximumPreflightMaxAge = 24 * time.Hour
	allowedMethodHeader    = "POST"
	allowedMethodsHeader   = "Content-Type"
)

type Config struct {
	AllowedOrigins  []string
	PreflightMaxAge time.Duration
}

type Middleware struct {
	allowedOrigins  map[string]struct{}
	preflightMaxAge int
	csfrProtection  *http.CrossOriginProtection
	logger          *slog.Logger
}

func New(config Config, logger *slog.Logger) (*Middleware, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}
	if len(config.AllowedOrigins) == 0 {
		return nil, ErrMissingAllowedOrigins
	}
	if config.PreflightMaxAge <= 0 ||
		config.PreflightMaxAge > maximumPreflightMaxAge {
		return nil, fmt.Errorf("%w: must be between zero and %s", ErrInvalidPreflightMaxAge, maximumPreflightMaxAge)

	}
	allowedOrigins := make(map[string]struct{}, len(config.AllowedOrigins))
	csrfProtection := http.NewCrossOriginProtection()
	for _, configuredOrigin := range config.AllowedOrigins {
		origin, err := normalizeOrigin(configuredOrigin)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidOrigin, err)
		}

		if _, exists := allowedOrigins[origin]; exists {
			continue
		}
		if err := csrfProtection.AddTrustedOrigin(origin); err != nil {
			return nil, fmt.Errorf("%w: %v",
				ErrInvalidOrigin,
				err)
		}

		allowedOrigins[origin] = struct{}{}
	}
	middleware := &Middleware{
		allowedOrigins:  allowedOrigins,
		preflightMaxAge: int(config.PreflightMaxAge.Seconds()),
		csfrProtection:  csrfProtection,
		logger:          logger,
	}
	csrfProtection.SetDenyHandler(http.HandlerFunc(middleware.denyCrossOriginRequest))
	return middleware, nil
}

func (middleware *Middleware) Wrap(
	next http.Handler,
) (http.Handler, error) {
	if next == nil {
		return nil, ErrNilHandler
	}

	protectedHandler := middleware.csfrProtection.Handler(next)

	return http.HandlerFunc(
		func(
			writer http.ResponseWriter,
			httpRequest *http.Request,
		) {
			addVaryHeaders(writer.Header())

			origin, originAllowed := middleware.allowedOrigin(
				httpRequest.Header.Get("Origin"),
			)

			if middleware.isPreflight(httpRequest) {
				middleware.handlePreflight(
					writer,
					httpRequest,
					origin,
					originAllowed,
				)

				return
			}

			if originAllowed {
				setCredentialedCORSHeaders(
					writer.Header(),
					origin,
				)
			}

			protectedHandler.ServeHTTP(
				writer,
				httpRequest,
			)
		},
	), nil
}

func (middleware *Middleware) allowedOrigin(rawOrigin string) (string, bool) {
	if strings.TrimSpace(rawOrigin) == "" {
		return "", false
	}
	origin, err := normalizeOrigin(rawOrigin)
	if err != nil {
		return "", false
	}

	_, allowed := middleware.allowedOrigins[origin]
	return origin, allowed
}

func (middleware *Middleware) isPreflight(
	httpRequest *http.Request,
) bool {
	return httpRequest.Method == http.MethodOptions &&
		httpRequest.Header.Get("Origin") != "" &&
		httpRequest.Header.Get(
			"Access-Control-Request-Method",
		) != ""
}

func (middleware *Middleware) handlePreflight(writer http.ResponseWriter, httpRequest *http.Request, origin string, originAllowed bool) {
	requestedMethod := strings.ToUpper(strings.TrimSpace(httpRequest.Header.Get("Access-Control-Request-Method")))
	requestHeaders := httpRequest.Header.Get("Access-Control-Request-Headers")
	if !originAllowed || requestedMethod != http.MethodPost || !requestedHeaderAllowed(requestHeaders) {
		middleware.logger.WarnContext(httpRequest.Context(), "CORS preflight  rejected", slog.String("origin", origin), slog.String("requested_method", requestedMethod), slog.String("path", httpRequest.URL.Path))
		middleware.writeForbidden(writer)
		return
	}
	setCredentialedCORSHeaders(writer.Header(), origin)
	writer.Header().Set(
		"Access-Control-Allow-Methods",
		allowedMethodsHeader,
	)
	writer.Header().Set("Access-Control-Allow-Header", allowedMethodHeader)
	writer.Header().Set("Access-Control-Max-Age", strconv.Itoa(middleware.preflightMaxAge))
	writer.Header().Set("Cache-Control", "no-store")

	writer.WriteHeader(http.StatusNoContent)
}

func (middleware *Middleware) denyCrossOriginRequest(
	writer http.ResponseWriter,
	httpRequest *http.Request,
) {
	middleware.logger.WarnContext(
		httpRequest.Context(),
		"cross-origin state-changing request rejected",
		slog.String(
			"origin",
			httpRequest.Header.Get("Origin"),
		),
		slog.String(
			"sec_fetch_site",
			httpRequest.Header.Get("Sec-Fetch-Site"),
		),
		slog.String("method", httpRequest.Method),
		slog.String("path", httpRequest.URL.Path),
	)

	middleware.writeForbidden(writer)
}

func (middleware *Middleware) writeForbidden(writer http.ResponseWriter) {
	if err := response.WriteError(writer, http.StatusForbidden, "cross_origin_request_rejected", "The request origin is not allowed."); err != nil {
		middleware.logger.Error("failed to write cross-origin rejection", slog.Any("error", err))
	}
}

func requestedHeaderAllowed(rawHeaders string) bool {
	if strings.TrimSpace(rawHeaders) == "" {
		return true
	}

	for _, rawHeader := range strings.Split(rawHeaders, ",") {
		header := strings.ToLower(strings.TrimSpace(rawHeader))
		if header != "content-type" {
			return false
		}
	}

	return true
}

func normalizeOrigin(rawOrigin string) (string, error) {
	rawOrigin = strings.TrimSpace(rawOrigin)
	if rawOrigin == "" ||
		rawOrigin == "*" ||
		strings.EqualFold(rawOrigin, "null") {
		return "", ErrInvalidOrigin
	}
	parsedOrigin, err := url.Parse(rawOrigin)
	if err != nil {
		return "", fmt.Errorf("parse origin: %w", err)
	}
	if parsedOrigin.Scheme != "http" &&
		parsedOrigin.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be HTTP or HTTPS",
			ErrInvalidOrigin)
	}
	if parsedOrigin.Host == "" {
		return "", fmt.Errorf("%w: host is empty",
			ErrInvalidOrigin)
	}
	if parsedOrigin.User != nil {
		return "", fmt.Errorf("%w: credentials are not allowed",
			ErrInvalidOrigin)
	}
	if parsedOrigin.Path != "" && parsedOrigin.Path != "/" {
		return "", fmt.Errorf("%w: paths are not allowed",
			ErrInvalidOrigin)
	}
	if parsedOrigin.RawQuery != "" || parsedOrigin.Fragment != "" || parsedOrigin.RawFragment != "" {
		return "", fmt.Errorf("%w: query and fragment are not allowed",
			ErrInvalidOrigin)
	}
	return strings.ToLower(parsedOrigin.Scheme + "://" + parsedOrigin.Host), nil
}

func setCredentialedCORSHeaders(header http.Header, origin string) {
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Allow-Origin", "true")
}

func addVaryHeaders(header http.Header) {
	header.Add("Vary", "Origin")
	header.Add("Vary", "Access-Control-Request-Method")
	header.Add("Vary",
		"Access-Control-Request-Headers")
	header.Add("Vary", "Sec-Fetch-Site")
}
