package provider

import (
	"context"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryConfig controls how doWithRetry behaves.
type RetryConfig struct {
	MaxAttempts int           // Total attempts including the first one; default 3.
	BaseDelay   time.Duration // Initial backoff delay; default 500ms.
	MaxDelay    time.Duration // Upper bound on computed delay; default 30s.
	JitterRatio float64       // Jitter as a fraction of BaseDelay (0.0-1.0); default 0.25.
}

// DefaultRetryConfig returns the standard retry configuration aligned with
// OpenClaw's retry behaviour (3 attempts, 500ms base, 30s cap, 25% jitter).
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
		JitterRatio: 0.25,
	}
}

// retryAfterCap is the maximum duration we honour from a Retry-After header.
const retryAfterCap = 60 * time.Second

// isRetriableStatus returns true for HTTP status codes that are safe to retry.
func isRetriableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,   // 429
		http.StatusRequestTimeout,      // 408
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}

// doWithRetry executes fn and retries on transient failures.
//
// fn must return an *http.Response (which the caller will read/close) and an
// error.  On each attempt the response is checked with shouldRetry; if nil the
// default isRetriableStatus is used.
//
// When a retry is needed the response body is drained and closed before
// sleeping, so the caller only ever receives a response that will NOT be
// retried.
//
// Backoff: delay = min(BaseDelay × 2^(attempt-1), MaxDelay) + jitter.
// A Retry-After header (seconds format) is respected when present and overrides
// the computed delay (capped at retryAfterCap).
func doWithRetry(
	ctx context.Context,
	cfg RetryConfig,
	fn func() (*http.Response, error),
	shouldRetry func(resp *http.Response) bool,
) (*http.Response, error) {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	if shouldRetry == nil {
		shouldRetry = func(resp *http.Response) bool {
			return isRetriableStatus(resp.StatusCode)
		}
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		// Check context before each attempt.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp, err := fn()
		if err != nil {
			// Network-level error — retriable.
			lastErr = err
			lastResp = nil
			if attempt < cfg.MaxAttempts-1 {
				delay := computeDelay(cfg, attempt)
				log.Printf("event=http_retry attempt=%d/%d delay=%s error=%q",
					attempt+1, cfg.MaxAttempts, delay, err)
				if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
					return nil, sleepErr
				}
			}
			continue
		}

		// Got a response — check whether it's retriable.
		if attempt < cfg.MaxAttempts-1 && shouldRetry(resp) {
			delay := computeDelay(cfg, attempt)
			if ra := parseRetryAfter(resp); ra > 0 {
				delay = ra
			}
			log.Printf("event=http_retry attempt=%d/%d delay=%s status=%d",
				attempt+1, cfg.MaxAttempts, delay, resp.StatusCode)
			// Drain and close the body so the connection can be reused.
			drainAndClose(resp)
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, sleepErr
			}
			lastErr = nil
			lastResp = nil
			continue
		}

		// Not retriable or final attempt — return as-is.
		return resp, nil
	}

	// All attempts exhausted.
	if lastResp != nil {
		return lastResp, lastErr
	}
	return nil, lastErr
}

// computeDelay calculates the backoff delay for a given attempt index (0-based).
//
//	delay = min(BaseDelay × 2^attempt, MaxDelay) + jitter
//	jitter = BaseDelay × JitterRatio × random()
func computeDelay(cfg RetryConfig, attempt int) time.Duration {
	exp := math.Pow(2, float64(attempt))
	delay := time.Duration(float64(cfg.BaseDelay) * exp)
	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	if cfg.JitterRatio > 0 {
		jitter := time.Duration(float64(cfg.BaseDelay) * cfg.JitterRatio * rand.Float64())
		delay += jitter
	}
	return delay
}

// parseRetryAfter extracts the Retry-After header value as a duration.
// Only the seconds-integer format is supported (RFC 7231 §7.1.3).
// Returns zero if the header is missing, unparseable, or non-positive.
// The result is capped at retryAfterCap.
func parseRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0
	}
	d := time.Duration(seconds) * time.Second
	if d > retryAfterCap {
		d = retryAfterCap
	}
	return d
}

// sleepWithContext pauses for d, returning early if ctx is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// drainAndClose reads the remaining response body and closes it so the
// underlying connection can be returned to the pool.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
