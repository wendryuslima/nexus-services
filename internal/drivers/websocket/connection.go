package websocket

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	coder "github.com/coder/websocket"
	"github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type connection struct {
	id          string
	identity    authentication.Identity
	hub         *Gateway
	user        *userState
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	socket      *coder.Conn
	queue       [][]byte
	queuedBytes int
	ready       bool
	notify      chan struct{}
	stop        chan struct{}
	closeOnce   sync.Once
	releaseOnce sync.Once
	code        coder.StatusCode
	reason      string
	createdAt   time.Time
}

func (c *connection) enqueue(data []byte) {
	c.mu.Lock()
	select {
	case <-c.stop:
		c.mu.Unlock()
		return
	default:
	}
	if len(c.queue) >= c.hub.config.QueueMessages || c.queuedBytes+len(data) > c.hub.config.QueueBytes {
		c.mu.Unlock()
		c.hub.metrics.QueueOverflows.Add(1)
		c.requestClose(coder.StatusTryAgainLater, "outbound queue full")
		return
	}
	c.queue = append(c.queue, data)
	c.queuedBytes += len(data)
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *connection) activate() {
	ready := struct {
		Version int    `json:"version"`
		Type    string `json:"type"`
		Data    struct {
			ConnectionID    string    `json:"connectionId"`
			UserID          string    `json:"userId"`
			AccessExpiresAt time.Time `json:"accessExpiresAt"`
		} `json:"data"`
	}{Version: ports.RealtimeProtocolVersion, Type: "connection.ready"}
	ready.Data.ConnectionID = c.id
	ready.Data.UserID = c.identity.UserID
	ready.Data.AccessExpiresAt = c.identity.AccessExpiresAt
	data, _ := json.Marshal(ready)
	c.mu.Lock()
	if len(c.queue) >= c.hub.config.QueueMessages || c.queuedBytes+len(data) > c.hub.config.QueueBytes {
		c.mu.Unlock()
		c.hub.metrics.QueueOverflows.Add(1)
		c.requestClose(coder.StatusTryAgainLater, "preparation buffer full")
		return
	}
	c.queue = append([][]byte{data}, c.queue...)
	c.queuedBytes += len(data)
	c.ready = true
	c.mu.Unlock()
	c.hub.metrics.ReadyMicroseconds.Add(time.Since(c.createdAt).Microseconds())
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func (c *connection) requestClose(code coder.StatusCode, reason string) {
	c.closeOnce.Do(func() { c.code = code; c.reason = reason; close(c.stop) })
}

func (c *connection) forceClose() {
	c.cancel()
	c.mu.Lock()
	socket := c.socket
	c.mu.Unlock()
	if socket != nil {
		_ = socket.CloseNow()
	}
}

func (c *connection) run(socket *coder.Conn) {
	c.mu.Lock()
	c.socket = socket
	c.mu.Unlock()
	socket.SetReadLimit(int64(c.hub.config.MaxMessageBytes))
	c.hub.metrics.Opened.Add(1)
	defer func() {
		c.forceClose()
		c.hub.metrics.Closed.Add(1)
		counter, _ := c.hub.metrics.ClosedByCode.LoadOrStore(int(c.code), &atomic.Int64{})
		counter.(*atomic.Int64).Add(1)
		c.hub.logger.Debug("realtime connection closed", "code", int(c.code), "reason", c.reason)
	}()
	var routines sync.WaitGroup
	routines.Go(c.readLoop)
	routines.Go(c.writeLoop)
	routines.Go(c.heartbeat)
	access := time.NewTimer(time.Until(c.identity.AccessExpiresAt))
	defer access.Stop()
	session := time.NewTimer(time.Until(c.identity.SessionExpiresAt))
	defer session.Stop()
	c.hub.prepare(c.user)
	readyTimer := time.NewTimer(c.hub.config.ReadyTimeout)
	defer readyTimer.Stop()
	select {
	case <-c.user.ready:
		c.hub.mu.Lock()
		err := c.user.err
		failed := c.user.failed
		c.hub.mu.Unlock()
		if err != nil || failed || !c.hub.subscriber.Healthy() {
			c.requestClose(coder.StatusTryAgainLater, "subscription unavailable")
		} else {
			c.activate()
		}
	case <-readyTimer.C:
		c.requestClose(coder.StatusTryAgainLater, "readiness timeout")
	case <-access.C:
		c.requestClose(coder.StatusCode(4001), "access expired")
	case <-session.C:
		c.requestClose(coder.StatusCode(4003), "session expired")
	case <-c.stop:
	case <-c.ctx.Done():
	}
	select {
	case <-access.C:
		c.requestClose(coder.StatusCode(4001), "access expired")
	case <-session.C:
		c.requestClose(coder.StatusCode(4003), "session expired")
	case <-c.stop:
	case <-c.ctx.Done():
	}
	routines.Wait()
}

func (c *connection) readLoop() {
	_, _, err := c.socket.Read(c.ctx)
	if err == nil {
		c.requestClose(coder.StatusPolicyViolation, "client commands are unsupported")
		return
	}
	c.requestClose(coder.StatusNormalClosure, "connection closed")
}

func (c *connection) writeLoop() {
	defer c.cancel()
	defer c.socket.CloseNow()
	for {
		select {
		case <-c.stop:
			_ = c.socket.Close(c.code, c.reason)
			return
		case <-c.ctx.Done():
			return
		case <-c.notify:
		}
		for {
			select {
			case <-c.stop:
				_ = c.socket.Close(c.code, c.reason)
				return
			default:
			}
			c.mu.Lock()
			if !c.ready || len(c.queue) == 0 {
				c.mu.Unlock()
				break
			}
			data := c.queue[0]
			c.queue[0] = nil
			c.queue = c.queue[1:]
			c.queuedBytes -= len(data)
			c.mu.Unlock()
			ctx, cancel := context.WithTimeout(c.ctx, c.hub.config.WriteTimeout)
			err := c.socket.Write(ctx, coder.MessageText, data)
			cancel()
			if err != nil {
				c.requestClose(coder.StatusTryAgainLater, "write failed")
				return
			}
		}
	}
}

func (c *connection) heartbeat() {
	ticker := time.NewTicker(c.hub.config.PingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, c.hub.config.PongTimeout)
			err := c.socket.Ping(ctx)
			cancel()
			if err != nil {
				c.requestClose(coder.StatusTryAgainLater, "heartbeat timeout")
				return
			}
		}
	}
}
