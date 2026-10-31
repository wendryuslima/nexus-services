package imagerepository

import (
	"context"
	"errors"
	"time"

	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository struct{ collection *mongo.Collection }

var _ ports.ImageRepository = (*Repository)(nil)

type document struct {
	ID              string    `bson:"_id"`
	UploadedBy      string    `bson:"uploaded_by"`
	ChatID          string    `bson:"chat_id"`
	ClientUploadID  string    `bson:"client_upload_id"`
	Bucket          string    `bson:"bucket"`
	ObjectKey       string    `bson:"object_key"`
	ContentType     string    `bson:"content_type"`
	Checksum        string    `bson:"checksum"`
	SizeBytes       int64     `bson:"size_bytes"`
	Width           int       `bson:"width"`
	Height          int       `bson:"height"`
	Status          string    `bson:"status"`
	MessageID       string    `bson:"message_id,omitempty"`
	UploadToken     string    `bson:"upload_token,omitempty"`
	DeleteToken     string    `bson:"delete_token,omitempty"`
	CreatedAt       time.Time `bson:"created_at"`
	ExpiresAt       time.Time `bson:"expires_at"`
	LockedUntil     time.Time `bson:"locked_until"`
	CleanupAfter    time.Time `bson:"cleanup_after"`
	DeleteStartedAt time.Time `bson:"delete_started_at,omitempty"`
}

func (d document) asset() *domainimage.Asset {
	return &domainimage.Asset{ID: d.ID, UploadedBy: d.UploadedBy, ChatID: d.ChatID, ClientUploadID: d.ClientUploadID, Bucket: d.Bucket, ObjectKey: d.ObjectKey, ContentType: d.ContentType, Checksum: d.Checksum, SizeBytes: d.SizeBytes, Width: d.Width, Height: d.Height, Status: d.Status, MessageID: d.MessageID, UploadToken: d.UploadToken, DeleteToken: d.DeleteToken, CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt, DeleteStartedAt: d.DeleteStartedAt}
}

func New(collection *mongo.Collection) (*Repository, error) {
	if collection == nil {
		return nil, errors.New("nil images collection")
	}
	return &Repository{collection: collection}, nil
}

func (r *Repository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "uploaded_by", Value: 1}, {Key: "client_upload_id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("image_upload_identity")},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "expires_at", Value: 1}, {Key: "locked_until", Value: 1}}, Options: options.Index().SetName("image_abandoned")},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "cleanup_after", Value: 1}, {Key: "locked_until", Value: 1}}, Options: options.Index().SetName("image_deletion_retry")},
	})
	return err
}

func (r *Repository) ReserveUpload(ctx context.Context, a domainimage.Asset, now time.Time) (*domainimage.Asset, bool, error) {
	d := document{ID: a.ID, UploadedBy: a.UploadedBy, ChatID: a.ChatID, ClientUploadID: a.ClientUploadID, Bucket: a.Bucket, ObjectKey: a.ObjectKey, ContentType: a.ContentType, Checksum: a.Checksum, SizeBytes: a.SizeBytes, Width: a.Width, Height: a.Height, Status: domainimage.Pending, UploadToken: a.UploadToken, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt, LockedUntil: now.Add(domainimage.UploadLease)}
	_, err := r.collection.InsertOne(ctx, d)
	if err == nil {
		return d.asset(), true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return nil, false, err
	}
	var existing document
	err = r.collection.FindOne(ctx, bson.M{"uploaded_by": a.UploadedBy, "client_upload_id": a.ClientUploadID}).Decode(&existing)
	if err != nil {
		return nil, false, err
	}
	if existing.ChatID != a.ChatID || existing.Checksum != a.Checksum || existing.SizeBytes != a.SizeBytes {
		return nil, false, ports.ErrImageUploadConflict
	}
	if existing.Status == domainimage.Ready || existing.Status == domainimage.Attached {
		if existing.Status == domainimage.Ready && !existing.ExpiresAt.After(now) {
			return nil, false, ports.ErrImageExpired
		}
		return existing.asset(), false, nil
	}
	if existing.Status != domainimage.Pending || !existing.ExpiresAt.After(now) {
		return nil, false, ports.ErrImageExpired
	}
	filter := bson.M{"_id": existing.ID, "status": domainimage.Pending, "expires_at": bson.M{"$gt": now}, "locked_until": bson.M{"$lte": now}}
	update := bson.M{"$set": bson.M{"upload_token": a.UploadToken, "locked_until": now.Add(domainimage.UploadLease)}}
	err = r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return existing.asset(), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return d.asset(), true, nil
}

func (r *Repository) MarkReady(ctx context.Context, id, token string, now time.Time) (bool, error) {
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "status": domainimage.Pending, "upload_token": token, "expires_at": bson.M{"$gt": now}, "locked_until": bson.M{"$gt": now}}, bson.M{"$set": bson.M{"status": domainimage.Ready, "locked_until": now}, "$unset": bson.M{"upload_token": ""}})
	if err != nil {
		return false, err
	}
	return result.ModifiedCount == 1, nil
}

func (r *Repository) ReleaseUpload(ctx context.Context, id, token string) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "status": domainimage.Pending, "upload_token": token}, bson.M{"$set": bson.M{"locked_until": time.Time{}}, "$unset": bson.M{"upload_token": ""}})
	return err
}

func (r *Repository) FindByID(ctx context.Context, id string) (*domainimage.Asset, error) {
	var d document
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ports.ErrImageNotFound
	}
	if err != nil {
		return nil, err
	}
	return d.asset(), nil
}

func (r *Repository) ClaimAbandoned(ctx context.Context, now time.Time, token string) (*domainimage.Asset, error) {
	filter := bson.M{"status": bson.M{"$in": bson.A{domainimage.Pending, domainimage.Ready}}, "expires_at": bson.M{"$lte": now}, "locked_until": bson.M{"$lte": now}}
	update := bson.M{"$set": bson.M{"status": domainimage.Deleting, "delete_token": token, "delete_started_at": now, "cleanup_after": now, "locked_until": now.Add(domainimage.UploadLease)}, "$unset": bson.M{"upload_token": ""}}
	var d document
	err := r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&d)
	if err == nil {
		return d.asset(), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}
	err = r.collection.FindOneAndUpdate(ctx, bson.M{"status": domainimage.Deleting, "cleanup_after": bson.M{"$lte": now}, "locked_until": bson.M{"$lte": now}}, bson.M{"$set": bson.M{"delete_token": token, "locked_until": now.Add(domainimage.UploadLease)}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.asset(), nil
}

func (r *Repository) CompleteDeletion(ctx context.Context, a domainimage.Asset, now time.Time) error {
	set := bson.M{"locked_until": now, "cleanup_after": a.DeleteStartedAt.Add(domainimage.DeletionGrace)}
	if !now.Before(a.DeleteStartedAt.Add(domainimage.DeletionGrace)) {
		set["status"] = domainimage.Deleted
		set["deleted_at"] = now
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": a.ID, "status": domainimage.Deleting, "delete_token": a.DeleteToken}, bson.M{"$set": set, "$unset": bson.M{"delete_token": ""}})
	return err
}
