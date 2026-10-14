package chats

import (
	"context"
	"encoding/json"
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
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := NewCreateMessageHandler(executor, logger)
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
