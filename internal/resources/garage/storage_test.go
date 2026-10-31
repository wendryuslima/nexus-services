package garage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
)

func TestGaragePrivatePutPresignedGetAndDelete(t *testing.T) {
	endpoint := os.Getenv("GARAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GARAGE_TEST_ENDPOINT and Garage test credentials")
	}
	c := config.ImagesConfig{Endpoint: endpoint, PublicEndpoint: endpoint, Region: "garage", Bucket: os.Getenv("GARAGE_TEST_BUCKET"), AccessKeyID: os.Getenv("GARAGE_TEST_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("GARAGE_TEST_SECRET_ACCESS_KEY")}
	if c.Bucket == "" {
		c.Bucket = "nexus-images"
	}
	storage, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	body := []byte("garage-private-integration-object")
	a := domainimage.Asset{Bucket: c.Bucket, ObjectKey: "tests/" + time.Now().Format("20060102150405.000000000"), ContentType: "image/png", SizeBytes: int64(len(body)), Checksum: strings.Repeat("a", 64)}
	if err := storage.Put(ctx, a, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = storage.Delete(cleanup, a.Bucket, a.ObjectKey)
	}()
	signed, err := storage.PresignGet(ctx, a, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil || u.Query().Get("X-Amz-Expires") != "600" {
		t.Fatal("presign expiry missing")
	}
	response, err := http.Get(signed)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(raw, body) || response.Header.Get("Content-Type") != "image/png" || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("signed GET failed: status=%d body length=%d", response.StatusCode, len(raw))
	}
	u.RawQuery = ""
	response, err = http.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("unsigned object is public: HTTP %d", response.StatusCode)
	}
	if err := storage.Delete(ctx, a.Bucket, a.ObjectKey); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(signed)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("deleted object remains available: HTTP %d", response.StatusCode)
	}
}
