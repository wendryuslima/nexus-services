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
	ChatID chat.ID
	Limit  int
	Before *TimelineCursor
}

type ListTimelineResult struct {
	Items      []*timeline.Item
	HasNext    bool
	NextCursor *TimelineCursor
}

type TimelineRepository interface {
	ListByChat(context.Context, ListTimelineParams) (ListTimelineResult, error)
}
