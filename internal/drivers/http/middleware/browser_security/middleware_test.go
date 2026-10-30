package browsersecurity

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitRetryAfterIsExposedToAllowedOrigin(t *testing.T) {
	middleware, err := New(Config{AllowedOrigins: []string{"https://app.example.com"}, PreflightMaxAge: time.Minute}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/chats/chat-1/messages", nil)
	request.Header.Set("Origin", "https://app.example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Access-Control-Expose-Headers") != "Retry-After" {
		t.Fatalf("browser cannot read Retry-After: status=%d headers=%v", recorder.Code, recorder.Header())
	}
}
