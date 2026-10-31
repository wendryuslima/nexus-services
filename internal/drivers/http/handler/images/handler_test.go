package images

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	applicationimages "github.com/wendryuslima/nexus-services/internal/application/images"
	domainimage "github.com/wendryuslima/nexus-services/internal/domain/image"
)

type serviceStub struct {
	input applicationimages.UploadInput
	bytes []byte
	calls int
}

func (s *serviceStub) Upload(_ context.Context, input applicationimages.UploadInput) (applicationimages.Metadata, error) {
	s.calls++
	s.input = input
	_, _ = input.Body.Seek(0, 0)
	s.bytes, _ = io.ReadAll(input.Body)
	return applicationimages.Metadata{ID: "image-1", Width: input.Width, Height: input.Height, ContentType: input.ContentType, SizeBytes: input.SizeBytes}, nil
}
func (s *serviceStub) Access(context.Context, string, string) (applicationimages.Access, error) {
	panic("unused")
}

type limiterStub struct{ wait time.Duration }

func (l limiterStub) Reserve(context.Context, string) (time.Duration, error) { return l.wait, nil }

func uploadRequest(t *testing.T, data []byte, extra bool) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chatId", "chat-1")
	_ = writer.WriteField("clientUploadId", "018f92d4-2c47-7c87-b33e-d2ea331d2ff2")
	part, err := writer.CreateFormFile("file", "untrusted-name.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	if extra {
		_ = writer.WriteField("chatId", "other")
	}
	_ = writer.Close()
	r := httptest.NewRequest("POST", "/v1/images", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestUploadValidatesBytesAndLimitsBeforeStorage(t *testing.T) {
	var pngData, jpegData, wideData bytes.Buffer
	_ = png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 3, 2)))
	_ = jpeg.Encode(&jpegData, image.NewRGBA(image.Rect(0, 0, 4, 2)), nil)
	_ = png.Encode(&wideData, image.NewNRGBA(image.Rect(0, 0, domainimage.MaxDimension+1, 1)))
	for _, test := range []struct {
		name        string
		data        []byte
		extra       bool
		status      int
		contentType string
	}{
		{"png", pngData.Bytes(), false, 200, "image/png"},
		{"jpeg", jpegData.Bytes(), false, 200, "image/jpeg"},
		{"html", []byte("<html>fake.png</html>"), false, 422, ""},
		{"truncated", pngData.Bytes()[:len(pngData.Bytes())/2], false, 422, ""},
		{"wide", wideData.Bytes(), false, 422, ""},
		{"oversize", make([]byte, domainimage.MaxBytes+1), false, 413, ""},
		{"duplicate field", pngData.Bytes(), true, 400, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &serviceStub{}
			handler, _ := New(service, limiterStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			w := httptest.NewRecorder()
			handler.upload(w, uploadRequest(t, test.data, test.extra), "sender")
			if w.Code != test.status {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			if test.status != 200 {
				if service.calls != 0 {
					t.Fatal("invalid image reached storage")
				}
				return
			}
			if service.input.UserID != "sender" || service.input.ChatID != "chat-1" || service.input.ContentType != test.contentType || service.input.Checksum != fmt.Sprintf("%x", sha256.Sum256(test.data)) || !bytes.Equal(service.bytes, test.data) {
				t.Fatalf("upload metadata/bytes changed: %+v", service.input)
			}
		})
	}
}

func TestUploadLimitsAndAuthenticationPrecedeFileParsing(t *testing.T) {
	service := &serviceStub{}
	handler, _ := New(service, limiterStub{wait: time.Second}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/v1/images", nil))
	if w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.upload(w, httptest.NewRequest("POST", "/v1/images", nil), "sender")
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" || service.calls != 0 {
		t.Fatalf("rate limit: %d %s", w.Code, w.Body.String())
	}
}
