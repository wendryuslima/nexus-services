package realtimesubscriber

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type Subscriber struct {
	conn         *nats.Conn
	maxBytes     int
	flushTimeout time.Duration
	healthy      atomic.Bool
	epoch        atomic.Uint64
	mu           sync.Mutex
	listeners    map[*subscription]func()
	cancel       context.CancelFunc
	done         chan struct{}
	logger       *slog.Logger
}

type subscription struct {
	owner *Subscriber
	sub   *nats.Subscription
	once  sync.Once
	err   error
}

func New(url string, maxBytes int, flushTimeout time.Duration, logger *slog.Logger) (*Subscriber, error) {
	if maxBytes <= 0 || flushTimeout <= 0 || logger == nil {
		return nil, errors.New("invalid subscriber configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Subscriber{maxBytes: maxBytes, flushTimeout: flushTimeout, listeners: make(map[*subscription]func()), cancel: cancel, done: make(chan struct{}), logger: logger}
	conn, err := nats.Connect(url, nats.Name("nexus-api-realtime"), nats.Timeout(flushTimeout), nats.RetryOnFailedConnect(true), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, _ error) { s.fail() }),
		nats.ReconnectHandler(func(_ *nats.Conn) { s.fail() }),
		nats.ClosedHandler(func(_ *nats.Conn) { s.fail() }),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, _ error) { s.fail() }))
	if err != nil {
		cancel()
		return nil, err
	}
	s.conn = conn
	go s.monitor(ctx)
	return s, nil
}

func (s *Subscriber) Healthy() bool { return s.healthy.Load() && s.conn.IsConnected() }

func (s *Subscriber) fail() {
	s.mu.Lock()
	s.epoch.Add(1)
	s.healthy.Store(false)
	callbacks := make([]func(), 0, len(s.listeners))
	for _, fn := range s.listeners {
		callbacks = append(callbacks, fn)
	}
	s.mu.Unlock()
	for _, fn := range callbacks {
		fn()
	}
}

func (s *Subscriber) monitor(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if s.conn.IsConnected() && !s.healthy.Load() {
			epoch := s.epoch.Load()
			flush, cancel := context.WithTimeout(ctx, s.flushTimeout)
			err := s.conn.FlushWithContext(flush)
			cancel()
			s.mu.Lock()
			if err == nil && epoch == s.epoch.Load() && s.conn.IsConnected() {
				s.healthy.Store(true)
			}
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Subscriber) Subscribe(ctx context.Context, userID string, onEvent func(ports.RealtimeMessageCreated), onFailure func()) (ports.RealtimeSubscription, error) {
	if !ports.ValidRealtimeUserID(userID) || onEvent == nil || onFailure == nil || !s.Healthy() {
		return nil, errors.New("realtime subscription unavailable")
	}
	epoch := s.epoch.Load()
	owned := &subscription{owner: s}
	s.mu.Lock()
	s.listeners[owned] = onFailure
	s.mu.Unlock()
	sub, err := s.conn.Subscribe("realtime.user."+userID, func(msg *nats.Msg) {
		var event ports.RealtimeMessageCreated
		if len(msg.Data) > s.maxBytes || json.Unmarshal(msg.Data, &event) != nil || ports.ValidateRealtimeMessage(event) != nil {
			s.logger.Warn("invalid realtime event rejected")
			onFailure()
			return
		}
		onEvent(event)
	})
	if err != nil {
		owned.Close()
		return nil, err
	}
	owned.sub = sub
	if err = sub.SetPendingLimits(100, s.maxBytes*100); err != nil {
		owned.Close()
		return nil, err
	}
	flush, cancel := context.WithTimeout(ctx, s.flushTimeout)
	defer cancel()
	if err = s.conn.FlushWithContext(flush); err != nil {
		owned.Close()
		return nil, err
	}
	if !s.Healthy() || epoch != s.epoch.Load() {
		owned.Close()
		return nil, errors.New("NATS changed during subscription")
	}
	return owned, nil
}

func (s *subscription) Close() error {
	s.once.Do(func() {
		s.owner.mu.Lock()
		delete(s.owner.listeners, s)
		s.owner.mu.Unlock()
		if s.sub != nil {
			s.err = s.sub.Unsubscribe()
		}
	})
	return s.err
}

func (s *Subscriber) Close(ctx context.Context) error {
	s.cancel()
	s.conn.Close()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
