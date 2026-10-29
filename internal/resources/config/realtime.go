package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type RealtimeConfig struct {
	NATSURL               string
	ReadyTimeout          time.Duration
	NATSFlushTimeout      time.Duration
	PingInterval          time.Duration
	PongTimeout           time.Duration
	WriteTimeout          time.Duration
	SessionCheckInterval  time.Duration
	SessionCheckTimeout   time.Duration
	ShutdownTimeout       time.Duration
	MaxMessageBytes       int
	QueueMessages         int
	QueueBytes            int
	MaxConnectionsPerUser int
	MaxConnections        int
}

func LoadRealtimeConfig() (RealtimeConfig, error) {
	c := RealtimeConfig{NATSURL: strings.TrimSpace(os.Getenv("NATS_URL")), ReadyTimeout: 10 * time.Second, NATSFlushTimeout: 5 * time.Second, PingInterval: 25 * time.Second, PongTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, SessionCheckInterval: 60 * time.Second, SessionCheckTimeout: 5 * time.Second, ShutdownTimeout: 10 * time.Second, MaxMessageBytes: 32768, QueueMessages: 100, QueueBytes: 1048576, MaxConnectionsPerUser: 10, MaxConnections: 5000}
	if c.NATSURL == "" {
		c.NATSURL = "nats://localhost:4222"
	}
	for key, target := range map[string]*time.Duration{"REALTIME_READY_TIMEOUT": &c.ReadyTimeout, "REALTIME_NATS_FLUSH_TIMEOUT": &c.NATSFlushTimeout, "REALTIME_PING_INTERVAL": &c.PingInterval, "REALTIME_PONG_TIMEOUT": &c.PongTimeout, "REALTIME_WRITE_TIMEOUT": &c.WriteTimeout, "REALTIME_SESSION_CHECK_INTERVAL": &c.SessionCheckInterval, "REALTIME_SESSION_CHECK_TIMEOUT": &c.SessionCheckTimeout, "REALTIME_SHUTDOWN_TIMEOUT": &c.ShutdownTimeout} {
		if raw := os.Getenv(key); raw != "" {
			v, err := time.ParseDuration(raw)
			if err != nil {
				return c, fmt.Errorf("invalid %s: %w", key, err)
			}
			*target = v
		}
	}
	for key, target := range map[string]*int{"REALTIME_MAX_MESSAGE_BYTES": &c.MaxMessageBytes, "REALTIME_OUTBOUND_QUEUE_MESSAGES": &c.QueueMessages, "REALTIME_OUTBOUND_QUEUE_BYTES": &c.QueueBytes, "REALTIME_MAX_CONNECTIONS_PER_USER": &c.MaxConnectionsPerUser, "REALTIME_MAX_CONNECTIONS_PER_INSTANCE": &c.MaxConnections} {
		if raw := os.Getenv(key); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				return c, fmt.Errorf("invalid %s: %w", key, err)
			}
			*target = v
		}
	}
	return c, c.Validate()
}

func (c RealtimeConfig) Validate() error {
	for _, v := range []time.Duration{c.ReadyTimeout, c.NATSFlushTimeout, c.PingInterval, c.PongTimeout, c.WriteTimeout, c.SessionCheckInterval, c.SessionCheckTimeout, c.ShutdownTimeout} {
		if v < time.Millisecond || v > 10*time.Minute {
			return fmt.Errorf("realtime durations must be between 1ms and 10m")
		}
	}
	if c.ReadyTimeout < c.NATSFlushTimeout || c.PongTimeout >= c.PingInterval {
		return fmt.Errorf("invalid realtime readiness or heartbeat budget")
	}
	if c.MaxMessageBytes < 1024 || c.MaxMessageBytes > 1048576 || c.QueueMessages < 2 || c.QueueMessages > 10000 || c.QueueBytes < c.MaxMessageBytes || c.QueueBytes > 64*1048576 || c.MaxConnectionsPerUser < 1 || c.MaxConnections < c.MaxConnectionsPerUser || c.MaxConnections > 1000000 {
		return fmt.Errorf("invalid realtime resource limits")
	}
	if c.NATSURL == "" {
		return fmt.Errorf("realtime NATS URL cannot be empty")
	}
	return nil
}

func NormalizeHTTPOrigin(raw string) (string, error) { return normalizeOrigin(raw) }
