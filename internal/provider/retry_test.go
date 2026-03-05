package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.BaseDelay != 500*time.Millisecond {
		t.Fatalf("BaseDelay = %v, want 500ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Fatalf("MaxDelay = %v, want 30s", cfg.MaxDelay)
	}
	if cfg.JitterRatio != 0.25 {
		t.Fatalf("JitterRatio = %v, want 0.25", cfg.JitterRatio)
	}
}

func TestComputeDelay(t *testing.T) {
	cfg := RetryConfig{
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
		JitterRatio: 0, // no jitter for deterministic test
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 100 * time.Millisecond},  // 100ms × 2^0 = 100ms
		{1, 200 * time.Millisecond},  // 100ms × 2^1 = 200ms
		{2, 400 * time.Millisecond},  // 100ms × 2^2 = 400ms
		{3, 800 * time.Millisecond},  // 100ms × 2^3 = 800ms
		{4, 1 * time.Second},         // 100ms × 2^4 = 1600ms → capped at 1s
		{10, 1 * time.Second},        // far exceeds cap
	}
	for _, tt := range tests {
		got := computeDelay(cfg, tt.attempt)
		if got != tt.expected {
			t.Errorf("computeDelay(attempt=%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestComputeDelayJitter(t *testing.T) {
	cfg := RetryConfig{
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		JitterRatio: 0.5,
	}

	// Run multiple times to check jitter range.
	for i := 0; i < 50; i++ {
		delay := computeDelay(cfg, 0)
		base := 100 * time.Millisecond
		maxJitter := time.Duration(float64(base) * 0.5)
		if delay < base || delay > base+maxJitter {
			t.Errorf("delay = %v, want in [%v, %v]", delay, base, base+maxJitter)
		}
	}
}

func TestIsRetriableStatus(t *testing.T) {
	retriable := []int{408, 429, 500, 502, 503, 504}
	for _, code := range retriable {
		if !isRetriableStatus(code) {
			t.Errorf("isRetriableStatus(%d) = false, want true", code)
		}
	}

	nonRetriable := []int{200, 201, 400, 401, 403, 404, 405, 422}
	for _, code := range nonRetriable {
		if isRetriableStatus(code) {
			t.Errorf("isRetriableStatus(%d) = true, want false", code)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected time.Duration
	}{
		{"empty", "", 0},
		{"valid_seconds", "5", 5 * time.Second},
		{"large_capped", "120", retryAfterCap},
		{"zero", "0", 0},
		{"negative", "-1", 0},
		{"non_numeric", "invalid", 0},
		{"date_format_unsupported", "Wed, 21 Oct 2025 07:28:00 GMT", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tt.header != "" {
				resp.Header.Set("Retry-After", tt.header)
			}
			got := parseRetryAfter(resp)
			if got != tt.expected {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.expected)
			}
		})
	}
}

func TestParseRetryAfterNilResponse(t *testing.T) {
	if got := parseRetryAfter(nil); got != 0 {
		t.Errorf("parseRetryAfter(nil) = %v, want 0", got)
	}
}

func TestDoWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}))
	defer ts.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	resp, err := doWithRetry(context.Background(), cfg, func() (*http.Response, error) {
		return http.Get(ts.URL)
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("calls = %d, want 1", n)
	}
}

func TestDoWithRetry_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}))
	defer ts.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	resp, err := doWithRetry(context.Background(), cfg, func() (*http.Response, error) {
		return http.Get(ts.URL)
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("calls = %d, want 3", n)
	}
}

func TestDoWithRetry_ExhaustsAllAttempts(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	resp, err := doWithRetry(context.Background(), cfg, func() (*http.Response, error) {
		return http.Get(ts.URL)
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	// On exhaustion the last response is returned.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("calls = %d, want 3", n)
	}
}

func TestDoWithRetry_NonRetriableStatusNotRetried(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	resp, err := doWithRetry(context.Background(), cfg, func() (*http.Response, error) {
		return http.Get(ts.URL)
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("calls = %d, want 1 (should not retry 400)", n)
	}
}

func TestDoWithRetry_ContextCancellation(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first call returns.
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: 50 * time.Millisecond, MaxDelay: 100 * time.Millisecond}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := doWithRetry(ctx, cfg, func() (*http.Response, error) {
		return http.Get(ts.URL)
	}, nil)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if n := atomic.LoadInt32(&calls); n > 2 {
		t.Fatalf("calls = %d, expected ≤ 2 due to cancellation", n)
	}
}

func TestDoWithRetry_NetworkError(t *testing.T) {
	var calls int32
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	_, err := doWithRetry(context.Background(), cfg, func() (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return nil, fmt.Errorf("connection refused")
	}, nil)
	if err == nil {
		t.Fatal("expected error on network failure")
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("calls = %d, want 3", n)
	}
}

func TestDoWithRetry_RetryAfterHeader(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}))
	defer ts.Close()

	// Use tiny BaseDelay; the Retry-After of 1s should override it.
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Second}
	start := time.Now()
	resp, err := doWithRetry(context.Background(), cfg, func() (*http.Response, error) {
		return http.Get(ts.URL)
	}, nil)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Should have waited ~1s due to Retry-After header.
	if elapsed < 800*time.Millisecond {
		t.Errorf("elapsed = %v, expected ≥ 800ms due to Retry-After: 1", elapsed)
	}
}

func TestDoWithRetry_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	}))
	defer ts.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	resp, err := doWithRetry(context.Background(), cfg, func() (*http.Response, error) {
		return http.Get(ts.URL)
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Fatalf("calls = %d, want 2", n)
	}
}

func TestSleepWithContext(t *testing.T) {
	// Normal sleep.
	start := time.Now()
	err := sleepWithContext(context.Background(), 10*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Fatal("sleep returned too early")
	}

	// Cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = sleepWithContext(ctx, time.Hour)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}
