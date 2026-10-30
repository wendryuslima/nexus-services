package chats

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	applicationauth "github.com/wendryuslima/nexus-services/internal/application/auth"
	applicationmessages "github.com/wendryuslima/nexus-services/internal/application/messages"
	authenticationmiddleware "github.com/wendryuslima/nexus-services/internal/drivers/http/middleware/authentication"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type createMessageExecutorStub struct {
	input  applicationmessages.CreateInput
	output applicationmessages.CreateOutput
	err    error
	calls  int
}

func (stub *createMessageExecutorStub) Execute(
	_ context.Context,
	input applicationmessages.CreateInput,
) (applicationmessages.CreateOutput, error) {
	stub.input = input
	stub.calls++
	return stub.output, stub.err
}

type createMessageAuthenticationStub struct {
	output applicationauth.AuthenticateOutput
}

func (stub createMessageAuthenticationStub) Execute(
	context.Context,
	applicationauth.AuthenticateInput,
) (applicationauth.AuthenticateOutput, error) {
	return stub.output, nil
}

type createMessageAccessTokenReaderStub struct{}

func (createMessageAccessTokenReaderStub) AccessToken(*http.Request) (string, error) {
	return "access-token", nil
}

func TestCreateMessageHandlerCreatesAndReturnsAMessage(t *testing.T) {
	createdAt := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	executor := &createMessageExecutorStub{output: applicationmessages.CreateOutput{
		ID: "message-1", ClientMessageID: "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
		ChatID: "chat-1", Sequence: 1, SenderID: "user-1",
		Content:   applicationmessages.CreatedContent{Type: "TEXT", Value: "Olá!"},
		CreatedAt: createdAt, DeliveryStatus: applicationmessages.DeliveryStatusPersisted, Created: true,
	}}
	handler := newAuthenticatedCreateMessageHandler(t, executor)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chats/chat-1/messages",
		strings.NewReader(`{"clientMessageId":"018f92d4-2c47-7c87-b33e-d2ea331d2ff2","content":{"type":"TEXT","value":"Olá!"}}`),
	)
	request.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if executor.calls != 1 || executor.input.CurrentUserID != "user-1" ||
		executor.input.ChatID != "chat-1" ||
		executor.input.Content.Value != "Olá!" {
		t.Fatalf("unexpected executor input: %+v", executor.input)
	}

	var payload struct {
		Data struct {
			ID             string `json:"id"`
			Sequence       int64  `json:"sequence"`
			DeliveryStatus string `json:"deliveryStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.ID != "message-1" || payload.Data.Sequence != 1 ||
		payload.Data.DeliveryStatus != "PERSISTED" {
		t.Fatalf("unexpected response payload: %+v", payload)
	}
}

func TestCreateMessageHandlerReturnsOKForAnIdempotentRetry(t *testing.T) {
	executor := &createMessageExecutorStub{output: applicationmessages.CreateOutput{
		ID: "message-1", ClientMessageID: "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
		ChatID: "chat-1", Sequence: 1, SenderID: "user-1",
		Content:        applicationmessages.CreatedContent{Type: "TEXT", Value: "Olá!"},
		CreatedAt:      time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		DeliveryStatus: applicationmessages.DeliveryStatusPersisted,
		Created:        false,
	}}
	handler := newAuthenticatedCreateMessageHandler(t, executor)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chats/chat-1/messages",
		strings.NewReader(`{"clientMessageId":"018f92d4-2c47-7c87-b33e-d2ea331d2ff2","content":{"type":"TEXT","value":"Olá!"}}`),
	)
	request.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateMessageHandlerMapsIdempotencyConflicts(t *testing.T) {
	executor := &createMessageExecutorStub{err: applicationmessages.ErrIdempotencyConflict}
	handler := newAuthenticatedCreateMessageHandler(t, executor)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chats/chat-1/messages",
		strings.NewReader(`{"clientMessageId":"018f92d4-2c47-7c87-b33e-d2ea331d2ff2","content":{"type":"TEXT","value":"Olá!"}}`),
	)
	request.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateMessageHandlerRejectsUnknownFields(t *testing.T) {
	executor := &createMessageExecutorStub{}
	handler := newAuthenticatedCreateMessageHandler(t, executor)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/chats/chat-1/messages",
		strings.NewReader(`{"clientMessageId":"018f92d4-2c47-7c87-b33e-d2ea331d2ff2","senderId":"forbidden","content":{"type":"TEXT","value":"Olá!"}}`),
	)
	request.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest || executor.calls != 0 {
		t.Fatalf("expected 400 without use case calls, got status=%d calls=%d", recorder.Code, executor.calls)
	}
}

func newAuthenticatedCreateMessageHandler(
	t *testing.T,
	executor CreateMessageExecutor,
) http.Handler {
	return newAuthenticatedCreateMessageHandlerWithLimiter(t, executor, &messageRateLimiterStub{})
}

func newAuthenticatedCreateMessageHandlerWithLimiter(t *testing.T, executor CreateMessageExecutor, limiter ports.MessageRateLimiter) http.Handler {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := NewCreateMessageHandler(executor, limiter, logger)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	middleware, err := authenticationmiddleware.New(
		createMessageAuthenticationStub{output: applicationauth.AuthenticateOutput{
			UserID: "user-1", SessionID: "session-1",
		}},
		createMessageAccessTokenReaderStub{},
		logger,
	)
	if err != nil {
		t.Fatalf("create authentication middleware: %v", err)
	}
	wrappedHandler, err := middleware.Wrap(handler)
	if err != nil {
		t.Fatalf("wrap handler with authentication: %v", err)
	}

	return wrappedHandler
}

type messageRateLimiterStub struct {
	retryAfter time.Duration
	err        error
	userID     string
	calls      int
}

func (stub *messageRateLimiterStub) Reserve(_ context.Context, userID string) (time.Duration, error) {
	stub.calls++
	stub.userID = userID
	return stub.retryAfter, stub.err
}

func TestCreateMessageHandlerRateLimit(t *testing.T) {
	for _, test := range []struct {
		name   string
		wait   time.Duration
		header string
	}{
		{"fractional second", time.Nanosecond, "1"},
		{"burst limit", 10 * time.Second, "10"},
		{"round up", 10*time.Second + time.Nanosecond, "11"},
		{"minute limit", time.Minute, "60"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &createMessageExecutorStub{}
			limiter := &messageRateLimiterStub{retryAfter: test.wait}
			handler := newAuthenticatedCreateMessageHandlerWithLimiter(t, executor, limiter)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chats/chat-1/messages", strings.NewReader("invalid")))
			if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != test.header {
				t.Fatalf("expected 429 and Retry-After %s, got %d %v", test.header, recorder.Code, recorder.Header())
			}
			if executor.calls != 0 || limiter.calls != 1 || limiter.userID != "user-1" {
				t.Fatalf("unexpected calls: executor=%d limiter=%+v", executor.calls, limiter)
			}
			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Error.Code != "message_rate_limit_exceeded" || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected error response: %s %v", recorder.Body.String(), recorder.Header())
			}
		})
	}
}

func TestCreateMessageHandlerResumesAfterRateLimit(t *testing.T) {
	executor := &createMessageExecutorStub{output: applicationmessages.CreateOutput{Created: true}}
	limiter := &messageRateLimiterStub{retryAfter: time.Second}
	handler := newAuthenticatedCreateMessageHandlerWithLimiter(t, executor, limiter)
	for _, chatID := range []string{"chat-1", "chat-2"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chats/"+chatID+"/messages", nil))
		if recorder.Code != http.StatusTooManyRequests {
			t.Fatalf("expected shared user limit across chats, got %d", recorder.Code)
		}
	}
	limiter.retryAfter = 0
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chats/chat-2/messages", strings.NewReader(`{"clientMessageId":"018f92d4-2c47-7c87-b33e-d2ea331d2ff2","content":{"type":"TEXT","value":"Olá!"}}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || executor.calls != 1 || recorder.Header().Get("Retry-After") != "" {
		t.Fatalf("expected send after release, got %d calls=%d", recorder.Code, executor.calls)
	}
}

func TestCreateMessageHandlerRateLimiterUnavailable(t *testing.T) {
	executor := &createMessageExecutorStub{}
	handler := newAuthenticatedCreateMessageHandlerWithLimiter(t, executor, &messageRateLimiterStub{err: errors.New("database unavailable")})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chats/chat-1/messages", nil))
	if recorder.Code != http.StatusServiceUnavailable || executor.calls != 0 || !strings.Contains(recorder.Body.String(), "message_rate_limit_unavailable") {
		t.Fatalf("expected 503 without creating a message, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateMessageHandlerChecksIdentityBeforeRateLimit(t *testing.T) {
	limiter := &messageRateLimiterStub{}
	handler, err := NewCreateMessageHandler(&createMessageExecutorStub{}, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/chats/chat-1/messages", nil))
	if recorder.Code != http.StatusUnauthorized || limiter.calls != 0 {
		t.Fatalf("expected 401 without reserving quota, got status=%d calls=%d", recorder.Code, limiter.calls)
	}
}
