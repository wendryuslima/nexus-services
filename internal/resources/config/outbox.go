package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type OutboxConfig struct {
	Concurrency       int
	RecoveryInterval  time.Duration
	RecoveryBatchSize int
}

func LoadOutboxConfig() (OutboxConfig, error) {
	c := OutboxConfig{Concurrency: 4, RecoveryInterval: 5 * time.Second, RecoveryBatchSize: 100}
	for key, target := range map[string]*int{
		"OUTBOX_CONCURRENCY":         &c.Concurrency,
		"OUTBOX_RECOVERY_BATCH_SIZE": &c.RecoveryBatchSize,
	} {
		if raw, exists := os.LookupEnv(key); exists {
			value, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				return c, fmt.Errorf("invalid %s: %w", key, err)
			}
			*target = value
		}
	}
	interval, err := readDurationEnvironment("OUTBOX_RECOVERY_INTERVAL", c.RecoveryInterval)
	if err != nil {
		return c, err
	}
	c.RecoveryInterval = interval
	if c.Concurrency < 1 || c.Concurrency > 64 {
		return c, fmt.Errorf("OUTBOX_CONCURRENCY must be between 1 and 64")
	}
	if c.RecoveryBatchSize < 1 || c.RecoveryBatchSize > 1000 {
		return c, fmt.Errorf("OUTBOX_RECOVERY_BATCH_SIZE must be between 1 and 1000")
	}
	if c.RecoveryInterval < 100*time.Millisecond {
		return c, fmt.Errorf("OUTBOX_RECOVERY_INTERVAL must be at least 100ms")
	}
	return c, nil
}
