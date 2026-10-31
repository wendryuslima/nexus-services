package garage

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
)

type Storage struct {
	client *s3.Client
	signer *s3.PresignClient
}

var _ ports.ImageStorage = (*Storage)(nil)

func New(c config.ImagesConfig) (*Storage, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	newClient := func(endpoint string) *s3.Client {
		return s3.New(s3.Options{Region: c.Region, BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""), HTTPClient: &http.Client{Timeout: 30 * time.Second}, RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	}
	return &Storage{client: newClient(c.Endpoint), signer: s3.NewPresignClient(newClient(c.PublicEndpoint))}, nil
}

func (s *Storage) Put(ctx context.Context, a domainimage.Asset, body io.ReadSeeker) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(a.Bucket), Key: aws.String(a.ObjectKey), Body: body, ContentLength: aws.Int64(a.SizeBytes), ContentType: aws.String(a.ContentType), ContentDisposition: aws.String("inline"), CacheControl: aws.String("private, no-store"), Metadata: map[string]string{"sha256": a.Checksum}})
	return err
}
func (s *Storage) Delete(ctx context.Context, bucket, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	return err
}
func (s *Storage) PresignGet(ctx context.Context, a domainimage.Asset, ttl time.Duration) (string, error) {
	request, err := s.signer.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.Bucket), Key: aws.String(a.ObjectKey)}, func(o *s3.PresignOptions) { o.Expires = ttl })
	if err != nil {
		return "", err
	}
	return request.URL, nil
}
