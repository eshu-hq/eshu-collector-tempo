// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/transport"
)

const (
	// FailureInvalidConfig marks an activation config the collector cannot
	// run: a malformed request or target, or an unresolvable credential.
	// Retrying cannot help.
	FailureInvalidConfig = "invalid_config"
	// FailureInvalidEvidence marks provider data that cannot become a fact.
	FailureInvalidEvidence = "invalid_evidence"
	// FailureResultTooLarge marks a result that exceeds max_result_bytes.
	FailureResultTooLarge = "result_too_large"
	// FailureClaimDeadline marks a claim whose lease deadline passed before
	// collection finished.
	FailureClaimDeadline = "claim_deadline_exceeded"

	defaultRetryAfterSeconds          = 30
	defaultRateLimitRetryAfterSeconds = 60
)

// failureResult builds the SDK result for a failed claim: terminal when
// retrying cannot help, retryable otherwise. It never carries facts, and the
// result passes the SDK validator (retryable requires retry_after_seconds).
func failureResult(claim sdk.Claim, observedAt time.Time, class string, terminal bool, retryAfter time.Duration) sdk.Result {
	state := sdk.ResultRetryable
	status := sdk.Status{
		Class:           sdk.StatusFailure,
		FailureClass:    class,
		SourceLatencyMS: latencyMillis(time.Since(observedAt)),
	}
	if terminal {
		state = sdk.ResultTerminal
	} else {
		status.RetryAfterSeconds = retryAfterSeconds(retryAfter, class)
	}
	return sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           state,
		Claim:           claim,
		Generation:      sdk.Generation{ID: claim.GenerationID, ObservedAt: observedAt.UTC()},
		Statuses:        []sdk.Status{status},
	}
}

// terminalResult is the bounded terminal shape for bad configuration and
// oversized results: an explicit terminal state with a named class.
func terminalResult(claim sdk.Claim, observedAt time.Time, class string) sdk.Result {
	return failureResult(claim, observedAt, class, true, 0)
}

// DeadlineExceededResult is the retryable result for a claim that arrives
// with no lease time left. The host releases the claim for a bounded retry
// instead of the collector being killed mid-request.
func DeadlineExceededResult(claim sdk.Claim, observedAt time.Time) sdk.Result {
	return failureResult(claim, observedAt, FailureClaimDeadline, false, 0)
}

// providerResult maps a provider failure onto the host claim disposition:
// terminal failures end the claim, retryable ones release it for a bounded
// retry with the provider's guidance (or a class default).
func providerResult(claim sdk.Claim, observedAt time.Time, failure transport.ProviderFailure) sdk.Result {
	return failureResult(claim, observedAt, failure.FailureClass(), failure.TerminalFailure(), failure.RetryAfterDelay())
}

// isDeadlineExceeded reports a context error from the claim deadline, not a
// provider verdict.
func isDeadlineExceeded(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func retryAfterSeconds(guidance time.Duration, class string) int {
	if guidance > 0 {
		return int(guidance.Seconds()) + 1
	}
	if class == string(transport.FailureRateLimited) {
		return defaultRateLimitRetryAfterSeconds
	}
	return defaultRetryAfterSeconds
}

func latencyMillis(d time.Duration) int {
	if d < 0 {
		return 0
	}
	return int(d.Milliseconds())
}
