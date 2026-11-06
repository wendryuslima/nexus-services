package ports

import (
	"context"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
)

type TimelineCursor struct {
	ItemID    timeline.ID
	CreatedAt time.Time
}

type ListTimelineParams struct {
	SearchWords     []string
	AroundSequence  *int64
	ChatID          chat.ID
	Limit           int
	Before          *TimelineCursor
	SequenceOrder   bool
	BeforeSequence  *int64
	AfterSequence   *int64
	ThroughSequence *int64
}

type ListTimelineResult struct {
	HasOlder                bool
	HasNewer                bool
	Items                   []*timeline.Item
	HasNext                 bool
	NextCursor              *TimelineCursor
	SnapshotThroughSequence *int64
	NextBeforeSequence      *int64
	NextAfterSequence       *int64
}

type TimelineRepository interface {
	ListByChat(context.Context, ListTimelineParams) (ListTimelineResult, error)
}
