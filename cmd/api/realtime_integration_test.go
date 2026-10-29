package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	coder "github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/wendryuslima/nexus-services/internal/application/delivery"
	applicationoutbox "github.com/wendryuslima/nexus-services/internal/application/outbox"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"github.com/wendryuslima/nexus-services/internal/resources/clock"
	"github.com/wendryuslima/nexus-services/internal/resources/identifier"
	chatrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/chat_repository"
	outboxrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/outbox_repository"
	jetstreampublisher "github.com/wendryuslima/nexus-services/internal/resources/nats/jetstream_publisher"
	realtimepublisher "github.com/wendryuslima/nexus-services/internal/resources/nats/realtime_publisher"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestRealtimeEndToEnd(t *testing.T) {
	mongoURI, natsURL := os.Getenv("MONGODB_TEST_URI"), os.Getenv("NATS_TEST_URL")
	if mongoURI == "" || natsURL == "" {
		t.Skip("set MONGODB_TEST_URI and NATS_TEST_URL for full integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dbName := fmt.Sprintf("nexus_realtime_e2e_%d", time.Now().UnixNano())
	rawDB, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = rawDB.Database(dbName).Drop(cleanup)
		_ = rawDB.Disconnect(cleanup)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	for key, value := range map[string]string{
		"APP_ENV": "test", "MONGODB_URI": mongoURI, "MONGODB_DATABASE": dbName, "NATS_URL": natsURL,
		"HTTP_ADDRESS": address, "HTTP_ALLOWED_ORIGINS": "http://localhost:5172", "AUTH_COOKIE_SECURE": "false", "AUTH_COOKIE_SAME_SITE": "lax",
		"JWT_ISSUER": "nexus-test", "JWT_AUDIENCE": "nexus-web-test", "JWT_ACCESS_SECRET_BASE64": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), "JWT_REFRESH_SECRET_BASE64": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		"REALTIME_SESSION_CHECK_INTERVAL": "50ms", "REALTIME_SESSION_CHECK_TIMEOUT": "1s", "REALTIME_SHUTDOWN_TIMEOUT": "2s",
	} {
		t.Setenv(key, value)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverContext, stopServer := context.WithCancel(ctx)
	serverDone := make(chan error, 1)
	go func() { serverDone <- run(serverContext, logger) }()
	t.Cleanup(func() {
		stopServer()
		select {
		case err := <-serverDone:
			if err != nil {
				t.Errorf("server shutdown: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("server leaked during shutdown")
		}
	})
	base := "http://" + address
	for {
		request, err := http.Get(base + "/missing")
		if err == nil {
			request.Body.Close()
			break
		}
		select {
		case err := <-serverDone:
			t.Fatalf("API startup failed: %v", err)
		case <-ctx.Done():
			t.Fatal("API did not start")
		case <-time.After(20 * time.Millisecond):
		}
	}
	alice, aliceID := e2eAccount(t, base, "alice@example.com")
	bob, bobID := e2eAccount(t, base, "bob@example.com")
	eve, _ := e2eAccount(t, base, "eve@example.com")
	chatRaw := e2eRequest(t, alice, "POST", base+"/v1/chats/direct", map[string]string{"relatedUserId": bobID})
	var chat struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(chatRaw, &chat); err != nil {
		t.Fatal(err)
	}
	if chat.Data.ID == "" {
		t.Fatal("chat identity missing")
	}
	forbidden, _ := http.NewRequestWithContext(ctx, "GET", base+"/v1/chats/"+chat.Data.ID+"/timeline?afterSequence=0", nil)
	response, err := eve.Do(forbidden)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("outsider read a private chat")
	}
	sessionRequest, _ := http.NewRequestWithContext(ctx, "GET", base+"/v1/auth/session", nil)
	response, err = alice.Do(sessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	var sessionResponse struct {
		Data struct {
			AccessExpiresAt time.Time `json:"accessExpiresAt"`
		} `json:"data"`
	}
	err = json.NewDecoder(response.Body).Decode(&sessionResponse)
	response.Body.Close()
	if err != nil || sessionResponse.Data.AccessExpiresAt.IsZero() || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("session endpoint did not expose verified expiry")
	}
	aliceSocket := e2eSocket(t, ctx, base, alice)
	defer aliceSocket.CloseNow()
	aliceTab := e2eSocket(t, ctx, base, alice)
	defer aliceTab.CloseNow()
	bobSocket := e2eSocket(t, ctx, base, bob)
	defer bobSocket.CloseNow()
	clientKey := "018f92d4-2c47-7c87-b33e-d2ea331d2ff2"
	body := map[string]any{"clientMessageId": clientKey, "content": map[string]string{"type": "TEXT", "value": "Olá em tempo real"}}
	messageRaw := e2eRequest(t, alice, "POST", base+"/v1/chats/"+chat.Data.ID+"/messages", body)
	var message struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.Unmarshal(messageRaw, &message)
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: "CHAT_EVENTS", Subjects: []string{"chat.message.created.>"}, Storage: jetstream.MemoryStorage})
	if err != nil {
		t.Fatal(err)
	}
	consumerName := fmt.Sprintf("E2E_DELIVERY_%d", time.Now().UnixNano())
	consumer, err := js.CreateOrUpdateConsumer(ctx, "CHAT_EVENTS", jetstream.ConsumerConfig{Durable: consumerName, DeliverPolicy: jetstream.DeliverNewPolicy, AckPolicy: jetstream.AckExplicitPolicy, FilterSubject: "chat.message.created.>"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = js.DeleteConsumer(cleanup, "CHAT_EVENTS", consumerName)
	}()
	outbox, _ := outboxrepository.NewRepository(rawDB.Database(dbName).Collection("outbox_events"))
	publisher, _ := jetstreampublisher.NewPublisher(js, 1)
	step, err := applicationoutbox.NewPublishOne(applicationoutbox.PublishOneDependencies{Store: outbox, Publisher: publisher, IDGenerator: identifier.NewUUIDGenerator(), Clock: clock.NewSystemClock(), WorkerID: "e2e", LeaseDuration: 10 * time.Second, PublishTimeout: 2 * time.Second, FinishTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := step.RunOnce(ctx); err != nil || outcome != applicationoutbox.OutcomePublished {
		t.Fatalf("outbox publication: %s %v", outcome, err)
	}
	queued, err := consumer.Next(jetstream.FetchMaxWait(2 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var internal ports.MessageCreatedEvent
	if err := json.Unmarshal(queued.Data(), &internal); err != nil {
		t.Fatal(err)
	}
	chats, _ := chatrepository.NewRepository(rawDB.Database(dbName).Collection("chats"))
	realtime, _ := realtimepublisher.NewPublisher(nc)
	dispatcher, _ := delivery.NewDispatchMessageCreated(chats, realtime)
	if err := dispatcher.Dispatch(ctx, internal); err != nil {
		t.Fatal(err)
	}
	if err := queued.DoubleAck(ctx); err != nil {
		t.Fatal(err)
	}
	for _, socket := range []*coder.Conn{aliceSocket, aliceTab, bobSocket} {
		readCtx, stop := context.WithTimeout(ctx, 3*time.Second)
		_, raw, err := socket.Read(readCtx)
		stop()
		if err != nil {
			t.Fatal(err)
		}
		var public ports.RealtimeMessageCreated
		if err := json.Unmarshal(raw, &public); err != nil {
			t.Fatal(err)
		}
		if public.Version != 1 || public.Data.ClientMessageID != clientKey || public.Data.ID != message.Data.ID || public.Data.SenderID != aliceID {
			t.Fatalf("invalid public notification: %+v", public)
		}
	}
	e2eRequest(t, alice, "POST", base+"/v1/chats/"+chat.Data.ID+"/messages", body)
	if outcome, err := step.RunOnce(ctx); err != nil || outcome != applicationoutbox.OutcomeNoWork {
		t.Fatalf("idempotent retry created an event: %s %v", outcome, err)
	}
	timelineRaw := e2eRequest(t, alice, "GET", base+"/v1/chats/"+chat.Data.ID+"/timeline?afterSequence=0", nil)
	var timeline struct {
		Data struct {
			Snapshot int64 `json:"snapshotThroughSequence"`
			Items    []struct {
				Sequence int64 `json:"sequence"`
				Message  struct {
					ClientMessageID string `json:"clientMessageId"`
				} `json:"message"`
			} `json:"items"`
		} `json:"data"`
	}
	json.Unmarshal(timelineRaw, &timeline)
	if timeline.Data.Snapshot != 1 || len(timeline.Data.Items) != 1 || timeline.Data.Items[0].Message.ClientMessageID != clientKey {
		t.Fatal("HTTP recovery contract lost message data")
	}
	chatsRaw := e2eRequest(t, alice, "GET", base+"/v1/chats", nil)
	var listed struct {
		Data []struct {
			LastSequence int64 `json:"lastSequence"`
		} `json:"data"`
	}
	if err := json.Unmarshal(chatsRaw, &listed); err != nil || len(listed.Data) != 1 || listed.Data[0].LastSequence != 1 {
		t.Fatal("chat list lost persisted high-water mark")
	}
	e2eRequest(t, bob, "POST", base+"/v1/auth/logout", nil)
	readCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	_, _, err = bobSocket.Read(readCtx)
	if coder.CloseStatus(err) != coder.StatusCode(4003) {
		t.Fatalf("revoked session stayed connected: %v", err)
	}
}

func e2eRequest(t *testing.T, client *http.Client, method, target string, payload any) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "http://localhost:5172")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("%s %s: HTTP %d: %s", method, request.URL.Path, response.StatusCode, raw)
	}
	return raw
}

func e2eAccount(t *testing.T, base, email string) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	credentials := map[string]string{"email": email, "password": "StrongPassword123!"}
	e2eRequest(t, client, "POST", base+"/v1/auth/signup", credentials)
	raw := e2eRequest(t, client, "POST", base+"/v1/auth/signin", credentials)
	var result struct {
		Data struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	json.Unmarshal(raw, &result)
	if result.Data.UserID == "" {
		t.Fatal("signin did not return user identity")
	}
	return client, result.Data.UserID
}

func e2eSocket(t *testing.T, ctx context.Context, base string, client *http.Client) *coder.Conn {
	t.Helper()
	parsed, _ := url.Parse(base + "/v1/realtime")
	request := &http.Request{Header: make(http.Header)}
	for _, cookie := range client.Jar.Cookies(parsed) {
		request.AddCookie(cookie)
	}
	request.Header.Set("Origin", "http://localhost:5172")
	for {
		socket, response, err := coder.Dial(ctx, "ws"+strings.TrimPrefix(parsed.String(), "http"), &coder.DialOptions{HTTPHeader: request.Header})
		if err == nil {
			readCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			_, raw, err := socket.Read(readCtx)
			stop()
			if err != nil {
				socket.CloseNow()
				t.Fatal(err)
			}
			var ready struct {
				Type string `json:"type"`
			}
			json.Unmarshal(raw, &ready)
			if ready.Type != "connection.ready" {
				socket.CloseNow()
				t.Fatal("ready must be first")
			}
			return socket
		}
		if response == nil || response.StatusCode != 503 {
			t.Fatalf("websocket handshake: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("gateway not ready")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
