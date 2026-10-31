package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type histogram struct {
	buckets []int64
	count   int64
	total   time.Duration
	maximum time.Duration
}

type latencyReport struct {
	Samples   int64   `json:"samples"`
	AverageMS float64 `json:"average_ms"`
	P50MS     float64 `json:"p50_ms"`
	P95MS     float64 `json:"p95_ms"`
	P99MS     float64 `json:"p99_ms"`
	MaxMS     float64 `json:"max_ms"`
}

type statistics struct {
	mu          sync.Mutex
	attempts    int64
	created     int64
	idempotent  int64
	rateLimited int64
	failures    int64
	statuses    map[string]int64
	errors      map[string]int64
	all         histogram
	persisted   histogram
	rejected    histogram
}

type loadReport struct {
	RunID              string           `json:"run_id"`
	BaseURL            string           `json:"base_url"`
	Users              int              `json:"users"`
	StartedAt          time.Time        `json:"started_at"`
	SetupSeconds       float64          `json:"setup_seconds"`
	RequestedSeconds   float64          `json:"requested_seconds"`
	ElapsedSeconds     float64          `json:"elapsed_seconds"`
	IntervalMS         float64          `json:"interval_ms"`
	MessageCharacters  int              `json:"message_characters"`
	RespectsRetryAfter bool             `json:"respects_retry_after"`
	Interrupted        bool             `json:"interrupted"`
	Attempts           int64            `json:"attempts"`
	Created            int64            `json:"created"`
	Idempotent         int64            `json:"idempotent"`
	RateLimited        int64            `json:"rate_limited"`
	Failures           int64            `json:"failures"`
	FailurePercent     float64          `json:"failure_percent"`
	RateLimitedPercent float64          `json:"rate_limited_percent"`
	AttemptsPerSecond  float64          `json:"attempts_per_second"`
	CreatedPerSecond   float64          `json:"created_per_second"`
	Statuses           map[string]int64 `json:"http_statuses"`
	Errors             map[string]int64 `json:"errors"`
	AllLatency         latencyReport    `json:"all_requests_latency"`
	PersistedLatency   latencyReport    `json:"persisted_requests_latency"`
	RejectedLatency    latencyReport    `json:"rate_limited_requests_latency"`
}

func newStatistics(timeout time.Duration) *statistics {
	size := int(math.Ceil(float64(timeout)/float64(time.Millisecond))) + 2
	return &statistics{statuses: map[string]int64{}, errors: map[string]int64{},
		all: histogram{buckets: make([]int64, size)}, persisted: histogram{buckets: make([]int64, size)}, rejected: histogram{buckets: make([]int64, size)}}
}

func (stats *statistics) record(response httpResult, latency time.Duration, clientMessageID, chatID string) {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.attempts++
	stats.all.add(latency)
	if response.status != 0 {
		stats.statuses[strconv.Itoa(response.status)]++
	}
	if response.err != nil {
		code := "transport_error"
		var networkError net.Error
		if errors.As(response.err, &networkError) && networkError.Timeout() {
			code = "timeout"
		}
		if errors.Is(response.err, context.Canceled) {
			code = "canceled"
		}
		stats.failures++
		stats.errors[code]++
		return
	}
	if response.status == http.StatusOK || response.status == http.StatusCreated {
		var payload struct {
			Data struct {
				ID              string `json:"id"`
				ClientMessageID string `json:"clientMessageId"`
				ChatID          string `json:"chatId"`
				Sequence        int64  `json:"sequence"`
			} `json:"data"`
		}
		if json.Unmarshal(response.body, &payload) != nil || payload.Data.ID == "" || payload.Data.ClientMessageID != clientMessageID || payload.Data.ChatID != chatID || payload.Data.Sequence <= 0 {
			stats.failures++
			stats.errors["invalid_success_response"]++
			return
		}
		if response.status == http.StatusCreated {
			stats.created++
		} else {
			stats.idempotent++
		}
		stats.persisted.add(latency)
		return
	}
	if response.status == http.StatusTooManyRequests && response.errorCode() == "message_rate_limit_exceeded" {
		stats.rateLimited++
		stats.rejected.add(latency)
		return
	}
	stats.failures++
	stats.errors[response.errorCode()]++
}

func (hist *histogram) add(value time.Duration) {
	index := min(int(math.Ceil(float64(value)/float64(time.Millisecond))), len(hist.buckets)-1)
	hist.buckets[max(0, index)]++
	hist.count++
	hist.total += value
	hist.maximum = max(hist.maximum, value)
}

func (hist histogram) percentile(percent float64) float64 {
	if hist.count == 0 {
		return 0
	}
	rank := int64(math.Ceil(float64(hist.count) * percent))
	var cumulative int64
	for index, count := range hist.buckets {
		cumulative += count
		if cumulative >= rank {
			if index == len(hist.buckets)-1 {
				return float64(hist.maximum) / float64(time.Millisecond)
			}
			return float64(index)
		}
	}
	return 0
}

func (hist histogram) report() latencyReport {
	result := latencyReport{Samples: hist.count, P50MS: hist.percentile(0.50), P95MS: hist.percentile(0.95), P99MS: hist.percentile(0.99), MaxMS: float64(hist.maximum) / float64(time.Millisecond)}
	if hist.count > 0 {
		result.AverageMS = float64(hist.total) / float64(time.Millisecond) / float64(hist.count)
	}
	return result
}

func (stats *statistics) report(runID string, config configuration, started time.Time, setup, elapsed time.Duration, interrupted bool) loadReport {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	result := loadReport{RunID: runID, BaseURL: config.baseURL, Users: config.users, StartedAt: started.UTC(), SetupSeconds: setup.Seconds(), RequestedSeconds: config.duration.Seconds(), ElapsedSeconds: elapsed.Seconds(), IntervalMS: float64(config.interval) / float64(time.Millisecond), MessageCharacters: config.messageSize, RespectsRetryAfter: !config.ignoreRateLimit, Interrupted: interrupted,
		Attempts: stats.attempts, Created: stats.created, Idempotent: stats.idempotent, RateLimited: stats.rateLimited, Failures: stats.failures, Statuses: stats.statuses, Errors: stats.errors,
		AllLatency: stats.all.report(), PersistedLatency: stats.persisted.report(), RejectedLatency: stats.rejected.report()}
	if stats.attempts > 0 {
		result.FailurePercent = 100 * float64(stats.failures) / float64(stats.attempts)
		result.RateLimitedPercent = 100 * float64(stats.rateLimited) / float64(stats.attempts)
	}
	if elapsed > 0 {
		result.AttemptsPerSecond = float64(stats.attempts) / elapsed.Seconds()
		result.CreatedPerSecond = float64(stats.created) / elapsed.Seconds()
	}
	return result
}
