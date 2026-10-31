package config

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func TestLoadOutboxConfigDefaults(t *testing.T) {
	for _, key := range []string{"OUTBOX_CONCURRENCY", "OUTBOX_RECOVERY_INTERVAL", "OUTBOX_RECOVERY_BATCH_SIZE"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	c, err := LoadOutboxConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Concurrency != 4 || c.RecoveryInterval != 5*time.Second || c.RecoveryBatchSize != 100 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadOutboxConfigOverridesAndRejectsInvalidLimits(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{
		{"OUTBOX_CONCURRENCY", "8", true}, {"OUTBOX_CONCURRENCY", "1", true},
		{"OUTBOX_CONCURRENCY", "0", false}, {"OUTBOX_CONCURRENCY", "65", false},
		{"OUTBOX_CONCURRENCY", "", false}, {"OUTBOX_CONCURRENCY", "many", false},
		{"OUTBOX_RECOVERY_BATCH_SIZE", "1", true}, {"OUTBOX_RECOVERY_BATCH_SIZE", "1001", false},
		{"OUTBOX_RECOVERY_INTERVAL", "100ms", true}, {"OUTBOX_RECOVERY_INTERVAL", "99ms", false},
		{"OUTBOX_RECOVERY_INTERVAL", "0s", false}, {"OUTBOX_RECOVERY_INTERVAL", "6m", false},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("OUTBOX_CONCURRENCY", "4")
			t.Setenv("OUTBOX_RECOVERY_INTERVAL", "5s")
			t.Setenv("OUTBOX_RECOVERY_BATCH_SIZE", "100")
			t.Setenv(tc.key, tc.value)
			c, err := LoadOutboxConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("config=%+v error=%v", c, err)
			}
			if tc.valid && tc.key == "OUTBOX_CONCURRENCY" {
				want, _ := strconv.Atoi(tc.value)
				if c.Concurrency != want {
					t.Fatalf("override ignored: %+v", c)
				}
			}
		})
	}
}
