package websocket

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	coder "github.com/coder/websocket"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/response"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
)

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	reject := func(status int, code string) {
		g.metrics.Rejected.Add(1)
		g.logger.Warn("realtime handshake rejected", "status", status, "code", code)
		_ = response.WriteError(w, status, code, "Não foi possível estabelecer a conexão em tempo real.")
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		reject(405, "method_not_allowed")
		return
	}
	if len(r.Header.Values("Origin")) != 1 {
		reject(403, "cross_origin_request_rejected")
		return
	}
	origin, err := config.NormalizeHTTPOrigin(r.Header.Get("Origin"))
	if _, allowed := g.origins[origin]; err != nil || !allowed {
		reject(403, "cross_origin_request_rejected")
		return
	}
	key, err := base64.StdEncoding.DecodeString(r.Header.Get("Sec-WebSocket-Key"))
	if r.ProtoMajor != 1 || !headerToken(r.Header, "Connection", "upgrade") || !headerToken(r.Header, "Upgrade", "websocket") || len(r.Header.Values("Sec-WebSocket-Version")) != 1 || r.Header.Get("Sec-WebSocket-Version") != "13" || len(r.Header.Values("Sec-WebSocket-Key")) != 1 || err != nil || len(key) != 16 || r.URL.RawQuery != "" {
		reject(400, "invalid_websocket_request")
		return
	}
	identity, ok := authentication.IdentityFromContext(r.Context())
	if !ok || !ports.ValidRealtimeUserID(identity.UserID) || identity.SessionID == "" || !identity.AccessExpiresAt.After(time.Now()) || !identity.SessionExpiresAt.After(time.Now()) {
		reject(401, "unauthenticated")
		return
	}
	c, err := g.reserve(identity)
	if err != nil {
		if errors.Is(err, errConnectionLimit) {
			reject(429, "realtime_connection_limit")
		} else {
			reject(503, "realtime_unavailable")
		}
		return
	}
	defer g.release(c)
	socket, err := coder.Accept(w, r, &coder.AcceptOptions{InsecureSkipVerify: true, CompressionMode: coder.CompressionDisabled})
	if err != nil {
		return
	}
	c.run(socket)
}

func headerToken(header http.Header, name, wanted string) bool {
	for _, value := range header.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), wanted) {
				return true
			}
		}
	}
	return false
}
