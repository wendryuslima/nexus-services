package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	coder "github.com/coder/websocket"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
)

var (
	errUnavailable     = errors.New("realtime unavailable")
	errConnectionLimit = errors.New("realtime connection limit")
)

type userState struct {
	id      string
	clients map[*connection]struct{}
	ready   chan struct{}
	started bool
	failed  bool
	err     error
	sub     ports.RealtimeSubscription
	ctx     context.Context
	cancel  context.CancelFunc
}

type attemptWindow struct {
	at    time.Time
	count int
}

type Metrics struct {
	ClosedByCode      sync.Map
	Opened            atomic.Int64
	Rejected          atomic.Int64
	Closed            atomic.Int64
	QueueOverflows    atomic.Int64
	Events            atomic.Int64
	ReadyMicroseconds atomic.Int64
}

type Gateway struct {
	config      config.RealtimeConfig
	origins     map[string]struct{}
	subscriber  ports.RealtimeSubscriber
	sessions    ports.SessionRepository
	ids         ports.IDGenerator
	logger      *slog.Logger
	mu          sync.Mutex
	users       map[string]*userState
	clients     map[*connection]struct{}
	perUser     map[string]int
	attempts    map[string]attemptWindow
	draining    bool
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	monitorDone chan struct{}
	metrics     Metrics
}

func New(cfg config.RealtimeConfig, origins []string, subscriber ports.RealtimeSubscriber, sessions ports.SessionRepository, ids ports.IDGenerator, logger *slog.Logger) (*Gateway, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if subscriber == nil || sessions == nil || ids == nil || logger == nil {
		return nil, errors.New("gateway dependencies cannot be nil")
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		normalized, err := config.NormalizeHTTPOrigin(origin)
		if err != nil {
			return nil, err
		}
		allowed[normalized] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil, errors.New("gateway requires explicit origins")
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{config: cfg, origins: allowed, subscriber: subscriber, sessions: sessions, ids: ids, logger: logger, users: make(map[string]*userState), clients: make(map[*connection]struct{}), perUser: make(map[string]int), attempts: make(map[string]attemptWindow), ctx: ctx, cancel: cancel, monitorDone: make(chan struct{})}
	go g.monitorSessions()
	return g, nil
}

func (g *Gateway) reserve(identity authentication.Identity) (*connection, error) {
	id, err := g.ids.New(g.ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.draining || !g.subscriber.Healthy() {
		return nil, errUnavailable
	}
	if len(g.clients) >= g.config.MaxConnections || g.perUser[identity.UserID] >= g.config.MaxConnectionsPerUser {
		return nil, errConnectionLimit
	}
	window := g.attempts[identity.UserID]
	if now.Sub(window.at) >= time.Minute {
		window = attemptWindow{at: now}
	}
	if window.count >= g.config.MaxConnectionsPerUser*6 {
		return nil, errConnectionLimit
	}
	if window.count == 0 && len(g.attempts) >= g.config.MaxConnections*4 {
		return nil, errConnectionLimit
	}
	window.count++
	g.attempts[identity.UserID] = window
	state := g.users[identity.UserID]
	if state == nil || state.failed {
		ctx, cancel := context.WithCancel(g.ctx)
		state = &userState{id: identity.UserID, clients: make(map[*connection]struct{}), ready: make(chan struct{}), ctx: ctx, cancel: cancel}
		g.users[identity.UserID] = state
	}
	ctx, cancel := context.WithCancel(g.ctx)
	c := &connection{id: id, identity: identity, hub: g, user: state, ctx: ctx, cancel: cancel, notify: make(chan struct{}, 1), stop: make(chan struct{}), createdAt: now}
	state.clients[c] = struct{}{}
	g.clients[c] = struct{}{}
	g.perUser[identity.UserID]++
	g.wg.Add(1)
	return c, nil
}

func (g *Gateway) release(c *connection) {
	c.releaseOnce.Do(func() {
		c.cancel()
		var sub ports.RealtimeSubscription
		g.mu.Lock()
		delete(g.clients, c)
		g.perUser[c.identity.UserID]--
		if g.perUser[c.identity.UserID] == 0 {
			delete(g.perUser, c.identity.UserID)
		}
		delete(c.user.clients, c)
		if len(c.user.clients) == 0 {
			c.user.cancel()
			sub = c.user.sub
			if g.users[c.identity.UserID] == c.user {
				delete(g.users, c.identity.UserID)
			}
		}
		g.mu.Unlock()
		if sub != nil {
			_ = sub.Close()
		}
		g.wg.Done()
	})
}

func (g *Gateway) prepare(state *userState) {
	g.mu.Lock()
	if state.started || g.draining {
		g.mu.Unlock()
		return
	}
	state.started = true
	g.wg.Add(1)
	g.mu.Unlock()
	go func() {
		defer g.wg.Done()
		ctx, cancel := context.WithTimeout(state.ctx, g.config.ReadyTimeout)
		defer cancel()
		sub, err := g.subscriber.Subscribe(ctx, state.id, func(event ports.RealtimeMessageCreated) { g.broadcast(state, event) }, func() { g.failUser(state) })
		g.mu.Lock()
		abandoned := len(state.clients) == 0 || g.draining || state.failed || !g.subscriber.Healthy()
		if err == nil && abandoned {
			err = errUnavailable
		}
		state.err = err
		if err == nil {
			state.sub = sub
		} else {
			state.failed = true
		}
		close(state.ready)
		g.mu.Unlock()
		if err != nil && sub != nil {
			_ = sub.Close()
		}
	}()
}

func (g *Gateway) broadcast(state *userState, event ports.RealtimeMessageCreated) {
	if ports.ValidateRealtimeMessage(event) != nil {
		g.failUser(state)
		return
	}
	data, err := json.Marshal(event)
	if err != nil || len(data) > g.config.MaxMessageBytes {
		g.failUser(state)
		return
	}
	g.mu.Lock()
	clients := make([]*connection, 0, len(state.clients))
	for c := range state.clients {
		clients = append(clients, c)
	}
	failed := state.failed
	g.mu.Unlock()
	if failed {
		return
	}
	g.metrics.Events.Add(1)
	for _, c := range clients {
		c.enqueue(data)
	}
}

func (g *Gateway) failUser(state *userState) {
	g.mu.Lock()
	state.failed = true
	clients := make([]*connection, 0, len(state.clients))
	for c := range state.clients {
		clients = append(clients, c)
	}
	g.mu.Unlock()
	for _, c := range clients {
		c.requestClose(coder.StatusTryAgainLater, "realtime unavailable")
	}
}

func (g *Gateway) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	g.draining = true
	clients := make([]*connection, 0, len(g.clients))
	for c := range g.clients {
		clients = append(clients, c)
	}
	for _, state := range g.users {
		state.failed = true
		state.cancel()
	}
	g.mu.Unlock()
	for _, c := range clients {
		c.requestClose(coder.StatusGoingAway, "server shutdown")
	}
	done := make(chan struct{})
	go func() { g.wg.Wait(); close(done) }()
	select {
	case <-done:
		g.cancel()
		select {
		case <-g.monitorDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		g.cancel()
		for _, c := range clients {
			c.forceClose()
		}
		return ctx.Err()
	}
}
