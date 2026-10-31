package outboxrepository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	statusPending    = "PENDING"
	statusProcessing = "PROCESSING"
)

type claimedContentDocument struct {
	Type    string `bson:"type"`
	Value   string `bson:"value"`
	ImageID string `bson:"imageId"`
	Caption string `bson:"caption"`
}

type claimedPayloadDocument struct {
	MessageID       string                 `bson:"messageId"`
	ClientMessageID string                 `bson:"clientMessageId"`
	ChatID          string                 `bson:"chatId"`
	Sequence        int64                  `bson:"sequence"`
	SenderID        string                 `bson:"senderId"`
	Content         claimedContentDocument `bson:"content"`
	CreatedAt       time.Time              `bson:"createdAt"`
}

type claimedEventDocument struct {
	ID            string                 `bson:"_id"`
	AggregateType string                 `bson:"aggregate_type"`
	AggregateID   string                 `bson:"aggregate_id"`
	EventType     string                 `bson:"event_type"`
	Payload       claimedPayloadDocument `bson:"payload"`
	Attempts      int64                  `bson:"attempts"`
	LeaseID       string                 `bson:"lease_id"`
}

func (document claimedEventDocument) toPort() *ports.OutboxEvent {
	return &ports.OutboxEvent{
		ID:            document.ID,
		AggregateType: document.AggregateType,
		AggregateID:   document.AggregateID,
		EventType:     document.EventType,
		Attempts:      document.Attempts,
		LeaseID:       document.LeaseID,

		Payload: ports.MessageCreatedPayload{
			MessageID: document.Payload.MessageID,

			ClientMessageID: document.Payload.ClientMessageID,

			ChatID:    document.Payload.ChatID,
			Sequence:  document.Payload.Sequence,
			SenderID:  document.Payload.SenderID,
			CreatedAt: document.Payload.CreatedAt,
			Content: ports.OutboxContent{
				Type:  document.Payload.Content.Type,
				Value: document.Payload.Content.Value, ImageID: document.Payload.Content.ImageID, Caption: document.Payload.Content.Caption,
			},
		},
	}
}

func (repository *Repository) ClaimNext(ctx context.Context, workerID string, leaseID string, now time.Time, leaseDuration time.Duration) (*ports.OutboxEvent, error) {
	if repository == nil || repository.collection == nil {
		return nil, ErrNilCollection
	}
	if strings.TrimSpace(workerID) == "" || strings.TrimSpace(leaseID) == "" || now.IsZero() || leaseDuration <= 0 {
		return nil, errors.New("invalid outbox claim parameters")
	}
	now = now.UTC()
	filter := bson.D{
		{Key: "status", Value: statusPending},
		{Key: "published_at", Value: nil},
		{Key: "next_attempt_at", Value: bson.D{
			{Key: "$lte", Value: now},
		}},
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "status", Value: statusProcessing},
			{Key: "locked_by", Value: workerID},
			{Key: "locked_until", Value: now.Add(leaseDuration)},

			{Key: "lease_id", Value: leaseID},
		}},
	}
	findOptions := options.FindOneAndUpdate().SetSort(bson.D{
		{Key: "next_attempt_at", Value: 1},
		{Key: "created_at", Value: 1},
		{Key: "_id", Value: 1},
	}).SetReturnDocument(options.After)

	var document claimedEventDocument
	err := repository.collection.FindOneAndUpdate(ctx, filter, update, findOptions).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next outbox event: %w", err)
	}
	return document.toPort(), nil
}
