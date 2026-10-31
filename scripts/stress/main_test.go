package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type simulatedAPI struct {
	mu            sync.Mutex
	users         map[string]string
	chats         map[string][]string
	senders       map[string]int
	messageIDs    map[string]bool
	active        atomic.Int64
	peak          atomic.Int64
	messageStatus int
	messageSize   int
}

func newSimulatedAPI(t *testing.T, status int) (*simulatedAPI, *httptest.Server) {
	t.Helper()
	api := &simulatedAPI{users: map[string]string{}, chats: map[string][]string{}, senders: map[string]int{}, messageIDs: map[string]bool{}, messageStatus: status, messageSize: 256}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Error("request must be a JSON POST")
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(request.URL.Path, "/v1/auth/") {
			var payload struct{ Email, Password string }
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if !strings.HasSuffix(payload.Email, "@example.test") || len(payload.Password) < 12 {
				t.Error("invalid test credentials")
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			if request.URL.Path == "/v1/auth/signup" {
				if _, found := api.users[payload.Email]; found {
					t.Error("duplicate test account")
				}
				api.users[payload.Email] = fmt.Sprintf("user-%d", len(api.users))
				writer.WriteHeader(http.StatusCreated)
			} else {
				id, found := api.users[payload.Email]
				if !found {
					t.Error("login before signup")
				}
				http.SetCookie(writer, &http.Cookie{Name: "session", Value: id, Path: "/", HttpOnly: true})
				json.NewEncoder(writer).Encode(map[string]any{"data": map[string]string{"user_id": id}})
			}
			return
		}
		cookie, err := request.Cookie("session")
		if err != nil {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if request.URL.Path == "/v1/chats/direct" {
			var payload struct {
				RelatedUserID string `json:"relatedUserId"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.RelatedUserID == "" || payload.RelatedUserID == cookie.Value {
				t.Error("invalid chat peer")
			}
			pair := []string{cookie.Value, payload.RelatedUserID}
			slices.Sort(pair)
			id := strings.Join(pair, "_")
			api.mu.Lock()
			api.chats[id] = pair
			api.mu.Unlock()
			json.NewEncoder(writer).Encode(map[string]any{"data": map[string]string{"id": id}})
			return
		}
		var payload struct {
			ClientMessageID string                       `json:"clientMessageId"`
			Content         struct{ Type, Value string } `json:"content"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		chatID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v1/chats/"), "/messages")
		api.mu.Lock()
		if !slices.Contains(api.chats[chatID], cookie.Value) {
			t.Error("user sent to a chat they do not belong to")
		}
		if api.messageIDs[payload.ClientMessageID] || !uuid.MatchString(payload.ClientMessageID) {
			t.Error("invalid or repeated clientMessageId")
		}
		api.messageIDs[payload.ClientMessageID] = true
		api.senders[cookie.Value]++
		api.mu.Unlock()
		if payload.Content.Type != "TEXT" || len(payload.Content.Value) != api.messageSize {
			t.Error("incorrect message payload")
		}
		active := api.active.Add(1)
		defer api.active.Add(-1)
		for old := api.peak.Load(); active > old; old = api.peak.Load() {
			if api.peak.CompareAndSwap(old, active) {
				break
			}
		}
		timer := time.NewTimer(3 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-request.Context().Done():
			return
		}
		if api.messageStatus == http.StatusTooManyRequests {
			writer.Header().Set("Retry-After", "1")
			writer.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(writer, `{"error":{"code":"message_rate_limit_exceeded"}}`)
			return
		}
		if api.messageStatus == http.StatusInternalServerError {
			writer.WriteHeader(http.StatusInternalServerError)
			io.WriteString(writer, `{"error":{"code":"internal_error"}}`)
			return
		}
		writer.WriteHeader(http.StatusCreated)
		json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{"id": payload.ClientMessageID, "clientMessageId": payload.ClientMessageID, "chatId": chatID, "sequence": 1}})
	}))
	t.Cleanup(server.Close)
	return api, server
}

func testConfiguration(t *testing.T, target string) configuration {
	t.Helper()
	return configuration{baseURL: target, users: 100, duration: 300 * time.Millisecond, interval: 20 * time.Millisecond, requestTimeout: 2 * time.Second, setupTimeout: 10 * time.Second, setupWorkers: 4, messageSize: 256, output: filepath.Join(t.TempDir(), "report.json")}
}

func readTestReport(t *testing.T, path string) loadReport {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report loadReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestStressUsesOneHundredIndependentUsersConcurrently(t *testing.T) {
	api, server := newSimulatedAPI(t, http.StatusCreated)
	config := testConfiguration(t, server.URL)
	if err := execute(context.Background(), config, io.Discard); err != nil {
		t.Fatal(err)
	}
	report := readTestReport(t, config.output)
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.users) != 100 || len(api.senders) != 100 || len(api.chats) != 50 {
		t.Fatalf("expected 100 authenticated senders in 50 chats; users=%d senders=%d chats=%d", len(api.users), len(api.senders), len(api.chats))
	}
	if api.peak.Load() < 2 {
		t.Fatal("message requests were not concurrent")
	}
	if report.Created < 100 || report.Attempts != report.Created || report.Failures != 0 || report.PersistedLatency.Samples != report.Created {
		t.Fatalf("invalid results: %+v", report)
	}
}

func TestStressRespectsRetryAfterAndSupportsOverload(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(fmt.Sprintf("ignore=%t", ignore), func(t *testing.T) {
			api, server := newSimulatedAPI(t, http.StatusTooManyRequests)
			config := testConfiguration(t, server.URL)
			config.users = 4
			config.ignoreRateLimit = ignore
			if err := execute(context.Background(), config, io.Discard); err != nil {
				t.Fatal(err)
			}
			report := readTestReport(t, config.output)
			api.mu.Lock()
			defer api.mu.Unlock()
			for user, attempts := range api.senders {
				if !ignore && attempts != 1 {
					t.Fatalf("user %s ignored Retry-After: %d attempts", user, attempts)
				}
				if ignore && attempts < 2 {
					t.Fatalf("user %s did not continue overload requests", user)
				}
			}
			if report.Failures != 0 || report.Created != 0 || report.RateLimited != report.Attempts {
				t.Fatalf("429 counted as an unexpected failure: %+v", report)
			}
		})
	}
}

func TestStressSavesFailureReport(t *testing.T) {
	_, server := newSimulatedAPI(t, http.StatusInternalServerError)
	config := testConfiguration(t, server.URL)
	config.users = 2
	if err := execute(context.Background(), config, io.Discard); err == nil {
		t.Fatal("expected failure exit result")
	}
	report := readTestReport(t, config.output)
	if report.Failures == 0 || report.Errors["internal_error"] != report.Failures || report.Created != 0 {
		t.Fatalf("server failures missing from report: %+v", report)
	}
}

func TestPersistedLatencyIsNotHiddenByFastRateLimitRejections(t *testing.T) {
	stats := newStatistics(time.Second)
	stats.record(httpResult{status: 201, body: []byte(`{"data":{"id":"message","clientMessageId":"client","chatId":"chat","sequence":1}}`)}, 900*time.Millisecond, "client", "chat")
	for range 99 {
		stats.record(httpResult{status: 429, body: []byte(`{"error":{"code":"message_rate_limit_exceeded"}}`)}, time.Millisecond, "", "")
	}
	report := stats.report("run", configuration{}, time.Now(), 0, time.Second, false)
	if report.AllLatency.P95MS != 1 || report.PersistedLatency.P95MS != 900 || report.RateLimited != 99 {
		t.Fatalf("rejections masked persisted latency: %+v", report)
	}
	stats.record(httpResult{status: 201, body: []byte(`{"data":{"id":"wrong"}}`)}, time.Millisecond, "client", "chat")
	stats.record(httpResult{err: context.DeadlineExceeded}, time.Second, "", "")
	if stats.failures != 2 || stats.errors["invalid_success_response"] != 1 || stats.errors["timeout"] != 1 {
		t.Fatalf("malformed responses or timeouts were counted as success: %+v", stats.errors)
	}
}

func TestStressStopsDuringBackoffAndSavesPartialResults(t *testing.T) {
	_, server := newSimulatedAPI(t, http.StatusTooManyRequests)
	config := testConfiguration(t, server.URL)
	config.users = 2
	config.duration = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := execute(ctx, config, io.Discard); err == nil {
		t.Fatal("expected interruption result")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("interruption did not stop backoff promptly")
	}
	report := readTestReport(t, config.output)
	if !report.Interrupted || report.Attempts == 0 {
		t.Fatalf("partial results missing: %+v", report)
	}
}
