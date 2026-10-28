// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestDeadlineExceededResultIsRetryable proves an expired lease releases the
// claim for a bounded retry with no facts attached.
func TestDeadlineExceededResultIsRetryable(t *testing.T) {
	t.Parallel()

	claim := testClaim()
	result := DeadlineExceededResult(claim, time.Now().UTC())
	if result.State != sdk.ResultRetryable {
		t.Fatalf("State = %q, want retryable", result.State)
	}
	if failureClass(result) != FailureClaimDeadline {
		t.Fatalf("failure class = %q, want %q", failureClass(result), FailureClaimDeadline)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want 0", len(result.Facts))
	}
}

// TestIsDeadlineExceededRecognizesLeaseErrors proves only context lease
// errors report as deadline expiry, not provider verdicts.
func TestIsDeadlineExceededRecognizesLeaseErrors(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"wrapped deadline", errors.Join(errors.New("fetch"), context.DeadlineExceeded), true},
		{"canceled", context.Canceled, true},
		{"provider verdict", errors.New("tempo 500: boom"), false},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			if got := isDeadlineExceeded(row.err); got != row.want {
				t.Fatalf("isDeadlineExceeded(%v) = %v, want %v", row.err, got, row.want)
			}
		})
	}
}

// TestRetryAfterSecondsPrefersGuidance proves provider retry guidance wins,
// rate limiting falls back to its class default, and everything else takes
// the standard default.
func TestRetryAfterSecondsPrefersGuidance(t *testing.T) {
	t.Parallel()

	if got := retryAfterSeconds(2*time.Second, "anything"); got != 3 {
		t.Fatalf("retryAfterSeconds(guidance) = %d, want 3", got)
	}
	if got := retryAfterSeconds(0, "rate_limited"); got != defaultRateLimitRetryAfterSeconds {
		t.Fatalf("retryAfterSeconds(rate_limited) = %d, want %d", got, defaultRateLimitRetryAfterSeconds)
	}
	if got := retryAfterSeconds(0, "terminal"); got != defaultRetryAfterSeconds {
		t.Fatalf("retryAfterSeconds(other) = %d, want %d", got, defaultRetryAfterSeconds)
	}
}

// TestLatencyMillisClampsNegative proves a clock skew never reports a
// negative source latency.
func TestLatencyMillisClampsNegative(t *testing.T) {
	t.Parallel()

	if got := latencyMillis(-time.Second); got != 0 {
		t.Fatalf("latencyMillis(negative) = %d, want 0", got)
	}
	if got := latencyMillis(1500 * time.Millisecond); got != 1500 {
		t.Fatalf("latencyMillis(1.5s) = %d, want 1500", got)
	}
}
