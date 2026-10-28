// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu-collector-tempo/tempo"
)

// TestHandleFailsClosedWithoutClockOrEnv proves the zero-value Handler
// resolves the clock and environment fallbacks, then fails a
// credential-dependent activation terminal before any provider contact.
func TestHandleFailsClosedWithoutClockOrEnv(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer
	handler := Handler{
		ClientFactory: func(tempo.TargetConfig) (tempo.EvidenceClient, error) {
			return stubTempoClient{snapshot: fixtureSnapshot()}, nil
		},
		Log: &logged,
	}
	result := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          testConfig(t),
	})
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for the unresolvable token", result.State)
	}
	if failureClass(result) != FailureInvalidConfig {
		t.Fatalf("failure class = %q, want %q", failureClass(result), FailureInvalidConfig)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want 0 on the fail-closed path", len(result.Facts))
	}
	var line struct {
		ComponentID  string `json:"component_id"`
		InstanceID   string `json:"instance_id"`
		FailureClass string `json:"failure_class"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(logged.Bytes()), &line); err != nil {
		t.Fatalf("log line is not one JSON object: %v", err)
	}
	if line.FailureClass != FailureInvalidConfig {
		t.Fatalf("logged failure_class = %q, want %q", line.FailureClass, FailureInvalidConfig)
	}
	if line.ComponentID == "" || line.InstanceID == "" {
		t.Fatalf("log line %+v carries no claim identity", line)
	}
}

// TestHandleRejectsUnbuildableSource proves an activation that parses but
// names no usable target fails terminal before any provider contact.
func TestHandleRejectsUnbuildableSource(t *testing.T) {
	t.Parallel()

	handler := testHandler(map[string]string{"TEMPO_TOKEN": "token-value"}, fixtureSnapshot())
	config := testConfig(t)
	config["targets"] = []any{map[string]any{
		"provider":    "tempo",
		"instance_id": "tempo-example",
		"base_url":    "https://tempo.example.test",
		"token_env":   "TEMPO_TOKEN",
		"enabled":     true,
	}}
	result := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	if result.State != sdk.ResultTerminal {
		t.Fatalf("State = %q, want terminal for the scopeless target", result.State)
	}
	if failureClass(result) != FailureInvalidConfig {
		t.Fatalf("failure class = %q, want %q", failureClass(result), FailureInvalidConfig)
	}
}

// TestHandleClassifiesProviderDeadline proves a provider-side deadline
// surfaces as a classified provider failure, never as the claim-lease fast
// path: Collect wraps every client error in transport.ProviderFailure, so
// the errors.As branch always wins over the isDeadlineExceeded arm.
func TestHandleClassifiesProviderDeadline(t *testing.T) {
	t.Parallel()

	handler := testHandler(map[string]string{"TEMPO_TOKEN": "token-value"}, fixtureSnapshot())
	handler.ClientFactory = func(tempo.TargetConfig) (tempo.EvidenceClient, error) {
		return stubTempoClient{err: context.DeadlineExceeded}, nil
	}
	result := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           testClaim(),
		Contract:        Contract(),
		Config:          testConfig(t),
	})
	if len(result.Facts) != 0 {
		t.Fatalf("facts = %d, want 0 on the provider failure path", len(result.Facts))
	}
	if len(result.Statuses) != 1 || result.Statuses[0].Class != sdk.StatusFailure {
		t.Fatalf("statuses = %+v, want one failure status", result.Statuses)
	}
	if failureClass(result) == FailureClaimDeadline {
		t.Fatal("provider deadline took the claim-lease fast path; want classified provider disposition")
	}
}
