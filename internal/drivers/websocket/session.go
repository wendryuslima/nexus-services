package websocket

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	coder "github.com/coder/websocket"
	"github.com/wendryuslima/nexus-services/internal/domain/session"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

func (g *Gateway) monitorSessions() {
	defer close(g.monitorDone)
	ticker := time.NewTicker(g.config.SessionCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.mu.Lock()
			groups := make(map[string][]*connection)
			for c := range g.clients {
				groups[c.identity.SessionID] = append(groups[c.identity.SessionID], c)
			}
			for id, window := range g.attempts {
				if time.Since(window.at) >= time.Minute {
					delete(g.attempts, id)
				}
			}
			active := len(g.clients)
			subscriptions := len(g.users)
			localClients := make([]*connection, 0, len(g.clients))
			for c := range g.clients {
				localClients = append(localClients, c)
			}
			g.mu.Unlock()
			queuedMessages, queuedBytes := 0, 0
			for _, c := range localClients {
				c.mu.Lock()
				queuedMessages += len(c.queue)
				queuedBytes += c.queuedBytes
				c.mu.Unlock()
			}
			closedByCode := make(map[int]int64)
			g.metrics.ClosedByCode.Range(func(key, value any) bool { closedByCode[key.(int)] = value.(*atomic.Int64).Load(); return true })
			g.logger.Info("realtime metrics", "active_connections", active, "subscriptions", subscriptions, "opened", g.metrics.Opened.Load(), "rejected", g.metrics.Rejected.Load(), "closed", g.metrics.Closed.Load(), "closed_by_code", closedByCode, "queued_messages", queuedMessages, "queued_bytes", queuedBytes, "queue_overflows", g.metrics.QueueOverflows.Load(), "events", g.metrics.Events.Load(), "ready_microseconds_total", g.metrics.ReadyMicroseconds.Load())
			if batch, ok := g.sessions.(ports.SessionBatchReader); ok {
				g.validateSessionBatch(batch, groups)
				continue
			}
			var reads sync.WaitGroup
			limit := make(chan struct{}, 16)
			for id, clients := range groups {
				select {
				case limit <- struct{}{}:
				case <-g.ctx.Done():
					reads.Wait()
					return
				}
				reads.Go(func() { defer func() { <-limit }(); g.validateSession(id, clients) })
			}
			reads.Wait()
		}
	}
}

func (g *Gateway) validateSessionBatch(reader ports.SessionBatchReader, groups map[string][]*connection) {
	ids := make([]session.ID, 0, len(groups))
	for raw := range groups {
		id, err := session.ParseID(raw)
		if err != nil {
			for _, c := range groups[raw] {
				c.requestClose(coder.StatusCode(4003), "invalid session")
			}
			continue
		}
		ids = append(ids, id)
	}
	ctx, cancel := context.WithTimeout(g.ctx, g.config.SessionCheckTimeout)
	defer cancel()
	sessions, err := reader.FindByIDs(ctx, ids)
	byID := make(map[string]*session.Session, len(sessions))
	for _, stored := range sessions {
		if stored != nil {
			byID[stored.ID().String()] = stored
		}
	}
	for id, clients := range groups {
		stored := byID[id]
		for _, c := range clients {
			if err != nil {
				c.requestClose(coder.StatusTryAgainLater, "session validation unavailable")
			} else if stored == nil || stored.EnsureActive(time.Now().UTC()) != nil || stored.UserID().String() != c.identity.UserID {
				c.requestClose(coder.StatusCode(4003), "session inactive")
			}
		}
	}
}

func (g *Gateway) validateSession(rawID string, clients []*connection) {
	id, err := session.ParseID(rawID)
	if err != nil {
		for _, c := range clients {
			c.requestClose(coder.StatusCode(4003), "invalid session")
		}
		return
	}
	ctx, cancel := context.WithTimeout(g.ctx, g.config.SessionCheckTimeout)
	defer cancel()
	stored, err := g.sessions.FindByID(ctx, id)
	code := coder.StatusCode(0)
	if errors.Is(err, ports.ErrSessionNotFound) {
		code = coder.StatusCode(4003)
	} else if err != nil || stored == nil {
		code = coder.StatusTryAgainLater
	} else if stored.EnsureActive(time.Now().UTC()) != nil {
		code = coder.StatusCode(4003)
	}
	for _, c := range clients {
		actual := code
		if actual == 0 && stored.UserID().String() != c.identity.UserID {
			actual = coder.StatusCode(4003)
		}
		if actual != 0 {
			c.requestClose(actual, "session validation ended")
		}
	}
}
