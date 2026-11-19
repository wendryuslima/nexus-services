package chatstream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const Name = "CHAT_EVENTS"

func Ensure(ctx context.Context, js jetstream.JetStream) error {
	if _, err := js.Stream(ctx, Name); err == nil {
		return nil
	} else if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return fmt.Errorf("inspect stream %s: %w", Name, err)
	}

	_, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:       Name,
		Subjects:   []string{"chat.message.>"},
		Storage:    jetstream.FileStorage,
		Replicas:   1,
		Retention:  jetstream.LimitsPolicy,
		Discard:    jetstream.DiscardOld,
		MaxAge:     7 * 24 * time.Hour,
		MaxBytes:   1 << 30,
		Duplicates: 2 * time.Minute,
	})
	if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {

		_, err = js.Stream(ctx, Name)
	}
	if err != nil {
		return fmt.Errorf("create stream %s: %w", Name, err)
	}
	return nil
}
