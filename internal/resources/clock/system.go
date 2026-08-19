package clock

import (
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
)

var _ ports.Clock = (*SystemClock)(nil)

type SystemClock struct{}

func NewSystemClock() *SystemClock {
	return &SystemClock{}
}

func (clock *SystemClock) Now() time.Time {
	return time.Now().UTC()
}
