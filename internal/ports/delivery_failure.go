package ports

import "context"

type DeliveryFailure struct {
	EventID        string `json:"eventId,omitempty"`
	StreamSequence uint64 `json:"streamSequence"`
	ReasonCode     string `json:"reasonCode"`
}

type DeliveryFailurePublisher interface {
	PublishFailure(ctx context.Context, failure DeliveryFailure) error
}
