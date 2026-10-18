package outboxrepository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const statusPublished = "PUBLISHED"
const statusFailed = "FAILED"

const maxStoredOutboxErrorLength = 512

func ownedProcessingFilter(eventID, leaseID string) bson.D {
	return bson.D{
		{Key: "_id", Value: eventID},
		{Key: "status", Value: statusProcessing},
		{Key: "lease_id", Value: leaseID},
		{Key: "published_at", Value: nil},
	}
}

func (repository *Repository) MarkFailed(ctx context.Context, eventID string, leaseID string, failedAt time.Time, reasonCode string) (bool, error) {
	if repository == nil || repository.collection == nil {
		return false, ErrNilCollection
	}

	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(leaseID) == "" || failedAt.IsZero() || strings.TrimSpace(reasonCode) == "" || len(reasonCode) > maxStoredOutboxErrorLength {
		return false, errors.New("invalid permanent failure parameters")
	}

	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "status", Value: statusFailed},
			{Key: "failed_at", Value: failedAt.UTC()},
			{Key: "last_error", Value: reasonCode},
			{Key: "locked_by", Value: nil},
			{Key: "locked_until", Value: nil},
		}},
		{Key: "$inc", Value: bson.D{

			{Key: "attempts", Value: int64(1)},
		}},
		{Key: "$unset", Value: bson.D{
			{Key: "lease_id", Value: ""},
		}},
	}

	result, err := repository.collection.UpdateOne(ctx, ownedProcessingFilter(eventID, leaseID), update)
	if err != nil {
		return false, fmt.Errorf("mark outbox event failed: %w", err)
	}

	return result.MatchedCount == 1, nil
}

func (repository *Repository) MarkPublished(ctx context.Context, eventID string, leaseID string, receipt ports.PublishReceipt, publishedAt time.Time) (bool, error) {
	if repository == nil || repository.collection == nil {
		return false, ErrNilCollection
	}
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(leaseID) == "" || strings.TrimSpace(receipt.Stream) == "" || receipt.Sequence == 0 || publishedAt.IsZero() {
		return false, errors.New("invalid outbox publication parameters")
	}

	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "status", Value: statusPublished},
			{Key: "published_at", Value: publishedAt.UTC()},

			{Key: "jetstream.stream", Value: receipt.Stream},
			{Key: "jetstream.sequence", Value: strconv.FormatUint(receipt.Sequence, 10)},

			{Key: "locked_by", Value: nil},
			{Key: "locked_until", Value: nil},
			{Key: "last_error", Value: nil},
		}},
		{Key: "$unset", Value: bson.D{
			{Key: "lease_id", Value: ""},
		}},
	}
	result, err := repository.collection.UpdateOne(ctx, ownedProcessingFilter(eventID, leaseID),
		update)
	if err != nil {
		return false, fmt.Errorf("mark outbox event published: %w", err)
	}
	return result.MatchedCount == 1, nil
}

func (repository *Repository) ScheduleRetry(ctx context.Context, eventID string, leaseID string, nextAttemptAt time.Time, sanitizedError string) (bool, error) {
	if repository == nil || repository.collection == nil {
		return false, ErrNilCollection
	}
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(leaseID) == "" || nextAttemptAt.IsZero() || strings.TrimSpace(sanitizedError) == "" || len(sanitizedError) > maxStoredOutboxErrorLength {
		return false, errors.New("invalid outbox retry parameters")
	}
	update := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "status", Value: statusPending},
			{Key: "next_attempt_at", Value: nextAttemptAt.UTC()},
			{Key: "last_error", Value: sanitizedError},

			{Key: "locked_by", Value: nil},
			{Key: "locked_until", Value: nil},
		}},
		{Key: "$inc", Value: bson.D{
			{Key: "attempts", Value: int64(1)},
		}},
		{Key: "$unset", Value: bson.D{
			{Key: "lease_id", Value: ""},
		}},
	}
	result, err := repository.collection.UpdateOne(ctx, ownedProcessingFilter(eventID, leaseID), update)
	if err != nil {
		return false, fmt.Errorf("schedule outbox event retry: %w", err)
	}
	return result.MatchedCount == 1, nil
}
