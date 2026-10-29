package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coder "github.com/coder/websocket"
	applicationauth "github.com/wendryuslima/nexus-services/internal/application/auth"
	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	authcookie "github.com/wendryuslima/nexus-services/internal/drivers/http/auth_cookie"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	browsersecurity "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/browser_security"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/recovery"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"github.com/wendryuslima/nexus-services/internal/resources/clock"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
	"github.com/wendryuslima/nexus-services/internal/resources/identifier"
)

type testSessions struct {
	ports.SessionRepository
	mu     sync.Mutex
	stored *session.Session
	err    error
}

func (s *testSessions) FindByID(context.Context, session.ID) (*session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stored, s.err
}
func (s *testSessions) setError(err error) { s.mu.Lock(); s.err = err; s.mu.Unlock() }

type testTokens struct {
	ports.TokenManager
	claims ports.TokenClaims
}

func (tokens testTokens) VerifyAccessToken(_ context.Context, raw string) (ports.TokenClaims, error) {
	if raw != "valid" {
		return ports.TokenClaims{}, ports.ErrInvalidToken
	}
	return tokens.claims, nil
}

type testSubscriber struct {
	calls  atomic.Int64
	closed atomic.Int64
	event  *ports.RealtimeMessageCreated
	err    error
	gate   <-chan struct{}
}
type testSubscription struct {
	owner *testSubscriber
	once  sync.Once
}

func (s *testSubscriber) Healthy() bool { return true }
func (s *testSubscriber) Subscribe(ctx context.Context, _ string, onEvent func(ports.RealtimeMessageCreated), _ func()) (ports.RealtimeSubscription, error) {
	s.calls.Add(1)
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.event != nil {
		onEvent(*s.event)
	}
	if s.err != nil {
		return nil, s.err
	}
	return &testSubscription{owner: s}, nil
}
func (s *testSubscription) Close() error { s.once.Do(func() { s.owner.closed.Add(1) }); return nil }

func gatewayFixture(t *testing.T, subscriber ports.RealtimeSubscriber, ttl time.Duration) (*Gateway, *httptest.Server, http.Header, *testSessions) {
	t.Helper()
	cfg, err := config.LoadRealtimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConnectionsPerUser = 2
	cfg.SessionCheckInterval = 20 * time.Millisecond
	now := time.Now().UTC()
	uid, _ := user.ParseID("user-1")
	sid, _ := session.ParseID("session-1")
	stored, err := session.New(sid, uid, "digest", now.Add(-time.Minute), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	sessions := &testSessions{stored: stored}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	g, err := New(cfg, []string{"http://localhost:5172"}, subscriber, sessions, identifier.NewUUIDGenerator(), logger)
	if err != nil {
		t.Fatal(err)
	}
	authenticator, err := applicationauth.NewAuthenticateUseCase(applicationauth.AuthenticateDependencies{SessionRepository: sessions, TokenManager: testTokens{claims: ports.TokenClaims{UserID: uid, SessionID: sid, ExpiresAt: now.Add(ttl)}}, Clock: clock.NewSystemClock()})
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := authcookie.NewManager(authcookie.Config{AccessName: "access", RefreshName: "refresh", Path: "/", SameSite: http.SameSiteLaxMode}, clock.NewSystemClock())
	if err != nil {
		t.Fatal(err)
	}
	auth, err := authentication.New(authenticator, cookies, logger)
	if err != nil {
		t.Fatal(err)
	}
	secured, err := auth.Wrap(g)
	if err != nil {
		t.Fatal(err)
	}
	browser, err := browsersecurity.New(browsersecurity.Config{AllowedOrigins: []string{"http://localhost:5172"}, PreflightMaxAge: time.Minute}, logger)
	if err != nil {
		t.Fatal(err)
	}
	secured, err = browser.Wrap(secured)
	if err != nil {
		t.Fatal(err)
	}
	recoverer, _ := recovery.New(logger)
	secured, err = recoverer.Wrap(secured)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(secured)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = g.Shutdown(ctx)
		server.Close()
	})
	return g, server, http.Header{"Origin": []string{"http://localhost:5172"}, "Cookie": []string{"access=valid"}}, sessions
}

func dialFixture(t *testing.T, server *httptest.Server, headers http.Header) *coder.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := coder.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &coder.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func readFrame(t *testing.T, conn *coder.Conn) map[string]json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]json.RawMessage
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func testEvent() ports.RealtimeMessageCreated {
	return ports.RealtimeMessageCreated{Version: 1, Type: "message.created", EventID: "event-1", OccurredAt: time.Now().UTC(), Data: ports.RealtimeMessageData{ID: "message-1", ChatID: "chat-1", Sequence: 1, SenderID: "user-2", CreatedAt: time.Now().UTC(), Content: ports.OutboxContent{Type: "TEXT", Value: "Olá"}}}
}

func TestReadyPrecedesConcurrentSubscriptionEvent(t *testing.T) {
	event := testEvent()
	subscriber := &testSubscriber{event: &event}
	_, server, headers, _ := gatewayFixture(t, subscriber, time.Hour)
	conn := dialFixture(t, server, headers)
	if string(readFrame(t, conn)["type"]) != `"connection.ready"` {
		t.Fatal("first event was not ready")
	}
	if string(readFrame(t, conn)["type"]) != `"message.created"` {
		t.Fatal("buffered event lost")
	}
}

func TestSharedSubscriptionAndAtomicConnectionLimit(t *testing.T) {
	subscriber := &testSubscriber{}
	_, server, headers, _ := gatewayFixture(t, subscriber, time.Hour)
	first := dialFixture(t, server, headers)
	readFrame(t, first)
	second := dialFixture(t, server, headers)
	readFrame(t, second)
	if subscriber.calls.Load() != 1 {
		t.Fatal("one user created multiple subscriptions")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, response, err := coder.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &coder.DialOptions{HTTPHeader: headers})
	if err == nil || response == nil || response.StatusCode != 429 {
		t.Fatalf("expected 429, got %v %v", response, err)
	}
}

func TestHandshakeRejectsOriginAndInvalidCookies(t *testing.T) {
	_, server, headers, _ := gatewayFixture(t, &testSubscriber{}, time.Hour)
	for _, origin := range []string{"", "null", "http://evil.example", "https://localhost:5172", "http://localhost:5173"} {
		t.Run(origin, func(t *testing.T) {
			h := headers.Clone()
			h.Set("Origin", origin)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, response, err := coder.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &coder.DialOptions{HTTPHeader: h})
			if conn != nil {
				conn.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != 403 {
				t.Fatalf("origin accepted: %q (%v)", origin, err)
			}
		})
	}
	headers.Set("Cookie", "access=invalid")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, response, err := coder.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &coder.DialOptions{HTTPHeader: headers})
	if err == nil || response == nil || response.StatusCode != 401 {
		t.Fatal("invalid cookie accepted")
	}
}

func TestSessionFailureCodesAndAccessExpiry(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure error
		ttl     time.Duration
		code    coder.StatusCode
	}{
		{"revoked", ports.ErrSessionNotFound, time.Hour, coder.StatusCode(4003)},
		{"database_unavailable", errors.New("database offline"), time.Hour, coder.StatusTryAgainLater},
		{"access_expired", nil, 100 * time.Millisecond, coder.StatusCode(4001)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, server, headers, sessions := gatewayFixture(t, &testSubscriber{}, test.ttl)
			conn := dialFixture(t, server, headers)
			readFrame(t, conn)
			if test.failure != nil {
				sessions.setError(test.failure)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _, err := conn.Read(ctx)
			if coder.CloseStatus(err) != test.code {
				t.Fatalf("expected %d got %v", test.code, err)
			}
		})
	}
}

func TestQueueOverflowAndShutdown(t *testing.T) {
	subscriber := &testSubscriber{}
	g, server, headers, _ := gatewayFixture(t, subscriber, time.Hour)
	c := &connection{hub: g, notify: make(chan struct{}, 1), stop: make(chan struct{})}
	for range g.config.QueueMessages + 1 {
		c.enqueue([]byte("event"))
	}
	select {
	case <-c.stop:
		if c.code != coder.StatusTryAgainLater {
			t.Fatal("wrong overflow code")
		}
	default:
		t.Fatal("overflow did not close")
	}
	conn := dialFixture(t, server, headers)
	readFrame(t, conn)
	readDone := make(chan error, 1)
	go func() { _, _, err := conn.Read(context.Background()); readDone <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if code := coder.CloseStatus(<-readDone); code != coder.StatusGoingAway {
		t.Fatalf("wrong shutdown code: %d", code)
	}
	if subscriber.closed.Load() != 1 {
		t.Fatal("subscription leaked")
	}
	g.mu.Lock()
	active := len(g.clients)
	g.mu.Unlock()
	if active != 0 {
		t.Fatal("socket leaked")
	}
}

func TestClientApplicationCommandsAreRejected(t *testing.T) {
	_, server, headers, _ := gatewayFixture(t, &testSubscriber{}, time.Hour)
	conn := dialFixture(t, server, headers)
	readFrame(t, conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, coder.MessageText, []byte(`{"type":"message.send"}`)); err != nil {
		t.Fatal(err)
	}
	_, _, err := conn.Read(ctx)
	if coder.CloseStatus(err) != coder.StatusPolicyViolation {
		t.Fatalf("expected policy violation: %v", err)
	}
}

func TestAccessExpiryAlsoAppliesWhilePreparing(t *testing.T) {
	gate := make(chan struct{})
	_, server, headers, _ := gatewayFixture(t, &testSubscriber{gate: gate}, 100*time.Millisecond)
	conn := dialFixture(t, server, headers)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	close(gate)
	if coder.CloseStatus(err) != coder.StatusCode(4001) {
		t.Fatalf("preparation ignored access expiry: %v", err)
	}
}

func TestConcurrentReservationsAndOldGenerationCleanup(t *testing.T) {
	g, _, _, _ := gatewayFixture(t, &testSubscriber{}, time.Hour)
	identity := authentication.Identity{UserID: "user-1", SessionID: "session-1", AccessExpiresAt: time.Now().Add(time.Hour), SessionExpiresAt: time.Now().Add(time.Hour)}
	results := make(chan *connection, 32)
	var attempts sync.WaitGroup
	for range 32 {
		attempts.Go(func() { c, _ := g.reserve(identity); results <- c })
	}
	attempts.Wait()
	close(results)
	clients := make([]*connection, 0, 2)
	for c := range results {
		if c != nil {
			clients = append(clients, c)
		}
	}
	if len(clients) != 2 {
		t.Fatalf("atomic limit violated: %d", len(clients))
	}
	for _, c := range clients {
		g.release(c)
	}
	old, err := g.reserve(identity)
	if err != nil {
		t.Fatal(err)
	}
	g.failUser(old.user)
	replacement, err := g.reserve(identity)
	if err != nil {
		g.release(old)
		t.Fatal(err)
	}
	g.release(old)
	g.mu.Lock()
	preserved := g.users[identity.UserID] == replacement.user
	g.mu.Unlock()
	g.release(replacement)
	if !preserved {
		t.Fatal("old cleanup deleted the new subscription generation")
	}
}
