package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"
)

type httpResult struct {
	status     int
	body       []byte
	retryAfter string
	err        error
}

func sendJSON(ctx context.Context, client *http.Client, config configuration, path string, payload any) httpResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return httpResult{err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return httpResult{err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "nexus-stress-test")
	if config.origin != "" {
		request.Header.Set("Origin", config.origin)
	}
	response, err := client.Do(request)
	if err != nil {
		return httpResult{err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if len(body) > 64*1024 {
		err = fmt.Errorf("resposta excedeu 64 KiB")
	}
	return httpResult{status: response.StatusCode, body: body, retryAfter: response.Header.Get("Retry-After"), err: err}
}

func (result httpResult) expect(statuses ...int) error {
	if result.err != nil {
		return result.err
	}
	if slices.Contains(statuses, result.status) {
		return nil
	}
	return fmt.Errorf("HTTP %d (%s)", result.status, result.errorCode())
}

func (result httpResult) errorCode() string {
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(result.body, &payload) == nil && payload.Error.Code != "" {
		return payload.Error.Code
	}
	return "unexpected_response"
}

func retryDelay(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return time.Second
}
